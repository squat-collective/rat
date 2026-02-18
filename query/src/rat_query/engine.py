"""DuckDB engine — long-lived connection for read-only queries."""

from __future__ import annotations

import logging
import re
import threading

import duckdb
import pyarrow as pa

from rat_query.config import DuckDBConfig, S3Config


def _to_arrow_table(arrow_result: pa.Table | pa.RecordBatchReader) -> pa.Table:
    """Convert a DuckDB .arrow() result to a PyArrow Table.

    DuckDB 1.0+ may return a RecordBatchReader instead of a Table from .arrow().
    This helper normalises both cases to a pa.Table.
    """
    if isinstance(arrow_result, pa.RecordBatchReader):
        return arrow_result.read_all()
    return arrow_result


logger = logging.getLogger(__name__)

# Strict regex for SQL identifiers — prevents injection via schema/table names
_SAFE_IDENTIFIER = re.compile(r"^[a-zA-Z_][a-zA-Z0-9_]*$")

# Only these schemas are allowed for table operations
_VALID_SCHEMAS = frozenset({"bronze", "silver", "gold"})

# SQL statements that are NOT allowed in user queries (case-insensitive)
_BLOCKED_STATEMENTS = re.compile(
    r"^\s*("
    r"INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|TRUNCATE|"
    r"COPY|ATTACH|DETACH|INSTALL|LOAD|IMPORT|EXPORT|"
    r"CALL|EXECUTE|PREPARE|DEALLOCATE|"
    r"SET|RESET|PRAGMA|CHECKPOINT|VACUUM|"
    r"GRANT|REVOKE"
    r")\b",
    re.IGNORECASE,
)

# DuckDB functions that allow direct file/URL/database access — blocked to
# prevent data exfiltration from the query sidecar.
_BLOCKED_FUNCTIONS = re.compile(
    r"\b("
    r"read_parquet|read_csv_auto|read_csv|read_json_auto|read_json|"
    r"read_text|read_blob|"
    r"parquet_scan|parquet_metadata|parquet_schema|"
    r"csv_scan|json_scan|"
    r"httpfs_|http_get|http_post|"
    r"postgres_scan|sqlite_scan|mysql_scan|"
    r"glob|read_ndjson_auto|read_ndjson"
    r")\s*\(",
    re.IGNORECASE,
)

# Maximum query length in characters (100KB) — prevents abuse via enormous queries.
_MAX_QUERY_LENGTH = 100_000

# Default query timeout in seconds — prevents runaway queries from consuming resources.
_DEFAULT_QUERY_TIMEOUT_SECONDS = 30


def _strip_sql_comments(sql: str) -> str:
    """Strip SQL comments so we can inspect the actual first statement keyword."""
    # Remove -- line comments
    result = re.sub(r"--[^\n]*", "", sql)
    # Remove /* ... */ block comments
    result = re.sub(r"/\*.*?\*/", "", result, flags=re.DOTALL)
    return result.strip()


def _validate_identifier(value: str, label: str = "identifier") -> str:
    """Validate that a string is a safe SQL identifier.

    Raises ValueError if the identifier contains unsafe characters.
    """
    if not _SAFE_IDENTIFIER.match(value):
        raise ValueError(f"Invalid {label}: {value!r}")
    return value


def _validate_schema(schema: str) -> str:
    """Validate that a schema is one of the allowed medallion layers."""
    if schema not in _VALID_SCHEMAS:
        raise ValueError(f"Invalid schema: {schema!r} (allowed: {_VALID_SCHEMAS})")
    return schema


def _quote_ns_table_refs(sql: str) -> str:
    """Auto-quote three-part namespace.layer.table references.

    Rewrites `word.layer.word` → `"word"."layer"."word"` where `layer` is a
    known medallion layer (bronze, silver, gold). This prevents parser errors
    when the namespace is a SQL reserved word (e.g. 'default').
    Skips references that are already quoted.
    """
    layers = "|".join(_VALID_SCHEMAS)
    return re.sub(
        rf"\b(\w+)\.({layers})\.(\w+)\b",
        r'"\1"."\2"."\3"',
        sql,
    )


class QueryEngine:
    """Wraps a long-lived DuckDB connection with S3 extensions configured.

    Unlike the runner engine (one connection per run), the query engine maintains
    a single persistent connection — DuckDB handles internal query parallelism.
    A threading.Lock protects DDL operations (view registration) only.
    """

    def __init__(self, s3_config: S3Config, duckdb_config: DuckDBConfig | None = None) -> None:
        self._s3_config = s3_config
        self._duckdb_config = duckdb_config or DuckDBConfig()
        self._conn = self._create_connection()
        self._ddl_lock = threading.Lock()

    def _create_connection(self) -> duckdb.DuckDBPyConnection:
        # S3 setup is intentionally aligned with runner/src/rat_runner/engine.py.
        # Keep both in sync when changing DuckDB S3 configuration. See task P6-01.
        conn = duckdb.connect(":memory:")
        conn.execute("INSTALL httpfs; LOAD httpfs;")
        conn.execute("INSTALL iceberg; LOAD iceberg;")
        conn.execute("SET s3_endpoint = ?", [self._s3_config.endpoint])
        conn.execute("SET s3_access_key_id = ?", [self._s3_config.access_key])
        conn.execute("SET s3_secret_access_key = ?", [self._s3_config.secret_key])
        conn.execute("SET s3_url_style = 'path'")
        conn.execute("SET s3_use_ssl = ?", [self._s3_config.use_ssl])
        conn.execute("SET s3_region = ?", [self._s3_config.region])
        if self._s3_config.session_token:
            conn.execute("SET s3_session_token = ?;", [self._s3_config.session_token])

        # Memory and thread limits to prevent resource exhaustion
        conn.execute("SET memory_limit = ?", [self._duckdb_config.memory_limit])
        conn.execute("SET threads = ?", [self._duckdb_config.threads])

        return conn

    def register_view(
        self,
        schema: str,
        name: str,
        s3_path: str,
        namespace: str | None = None,
    ) -> None:
        """Register a DuckDB view backed by parquet files at s3_path.

        Creates the view in two locations for query flexibility:
        1. "layer"."table" — allows `SELECT * FROM bronze.orders`
        2. "namespace"."layer"."table" — allows `SELECT * FROM default.bronze.orders`

        The namespace path uses DuckDB's ATTACH to create an in-memory catalog
        matching the namespace name.
        """
        _validate_schema(schema)
        _validate_identifier(name, "table name")
        # DuckDB does not support prepared parameters in DDL (CREATE VIEW).
        # The s3_path is built from validated schema/name components, so
        # we safely inline it with single-quote escaping.
        glob = f"{s3_path}/**/*.parquet".replace("'", "''")
        view_sql = f"SELECT * FROM read_parquet('{glob}', hive_partitioning=true)"
        with self._ddl_lock:
            # 1. Register under layer.table (default catalog)
            self._conn.execute(f'CREATE SCHEMA IF NOT EXISTS "{schema}"')
            self._conn.execute(
                f'CREATE OR REPLACE VIEW "{schema}"."{name}" AS {view_sql}',
            )
            # 2. Register under namespace.layer.table (attached catalog)
            if namespace:
                _validate_identifier(namespace, "namespace")
                self._conn.execute(
                    f"ATTACH IF NOT EXISTS ':memory:' AS \"{namespace}\"",
                )
                self._conn.execute(
                    f'CREATE SCHEMA IF NOT EXISTS "{namespace}"."{schema}"',
                )
                self._conn.execute(
                    f'CREATE OR REPLACE VIEW "{namespace}"."{schema}"."{name}" AS {view_sql}',
                )

    def drop_view(
        self,
        schema: str,
        name: str,
        namespace: str | None = None,
    ) -> None:
        """Drop a single view by schema and name (both default and namespace catalogs)."""
        _validate_schema(schema)
        _validate_identifier(name, "table name")
        with self._ddl_lock:
            self._conn.execute(f'DROP VIEW IF EXISTS "{schema}"."{name}"')
            if namespace:
                _validate_identifier(namespace, "namespace")
                self._conn.execute(
                    f'DROP VIEW IF EXISTS "{namespace}"."{schema}"."{name}"',
                )

    def drop_all_views(self) -> None:
        """Drop all user-created schemas (clean slate before re-registration)."""
        with self._ddl_lock:
            for schema_name in ("bronze", "silver", "gold"):
                self._conn.execute(f"DROP SCHEMA IF EXISTS {schema_name} CASCADE")

    def query_arrow(
        self,
        sql: str,
        limit: int = 1000,
        timeout_seconds: int = _DEFAULT_QUERY_TIMEOUT_SECONDS,
    ) -> pa.Table:
        """Execute SQL and return result as a PyArrow Table.

        Only SELECT/WITH queries are allowed. Wraps the query in a LIMIT clause.
        Auto-quotes namespace.layer.table references (e.g. default.bronze.orders
        → "default"."bronze"."orders") so reserved words like 'default' don't
        cause parser errors.

        Security checks (in order):
        1. Query length limit (_MAX_QUERY_LENGTH)
        2. Blocked statement keywords (INSERT, DROP, etc.)
        3. Blocked functions (read_parquet, http_get, etc.)
        """
        if len(sql) > _MAX_QUERY_LENGTH:
            raise ValueError(f"Query too long ({len(sql)} chars, max {_MAX_QUERY_LENGTH})")

        stripped = _strip_sql_comments(sql)
        if _BLOCKED_STATEMENTS.match(stripped):
            raise ValueError("Only SELECT queries are allowed")

        if _BLOCKED_FUNCTIONS.search(stripped):
            raise ValueError("Direct file/URL access functions are not allowed in queries")

        # Auto-quote three-part identifiers where the middle part is a medallion layer.
        # This handles reserved words (like 'default') used as namespace names.
        wrapped = _quote_ns_table_refs(sql.rstrip().rstrip(";"))
        if limit > 0:
            wrapped = f"SELECT * FROM ({wrapped}) AS _q LIMIT {limit}"

        # Set per-query timeout to prevent runaway queries (DuckDB 1.1+).
        has_timeout = False
        try:
            self._conn.execute(f"SET statement_timeout='{timeout_seconds}s'")
            has_timeout = True
        except duckdb.CatalogException:
            pass  # DuckDB version doesn't support statement_timeout
        try:
            result = self._conn.execute(wrapped)
            return _to_arrow_table(result.arrow())
        finally:
            if has_timeout:
                self._conn.execute("RESET statement_timeout")

    def describe_table(self, schema: str, name: str) -> list[tuple[str, str]]:
        """Return (column_name, column_type) pairs for a table/view."""
        _validate_schema(schema)
        _validate_identifier(name, "table name")
        result = self._conn.execute(f'DESCRIBE "{schema}"."{name}"')
        rows = result.fetchall()
        return [(row[0], row[1]) for row in rows]

    def count_rows(self, schema: str, name: str) -> int:
        """Return the number of rows in a table/view."""
        _validate_schema(schema)
        _validate_identifier(name, "table name")
        result = self._conn.execute(f'SELECT COUNT(*) FROM "{schema}"."{name}"')
        return result.fetchone()[0]  # type: ignore[index]

    def close(self) -> None:
        """Release the DuckDB connection."""
        if self._conn is not None:
            self._conn.close()
            self._conn = None  # type: ignore[assignment]
