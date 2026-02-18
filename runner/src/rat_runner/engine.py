"""DuckDB engine — one connection per run, S3 + Iceberg extensions."""

from __future__ import annotations

import duckdb
import pyarrow as pa

from rat_runner.config import DuckDBConfig, S3Config


def _to_arrow_table(arrow_result: pa.Table | pa.RecordBatchReader) -> pa.Table:
    """Convert a DuckDB .arrow() result to a PyArrow Table.

    DuckDB 1.0+ may return a RecordBatchReader instead of a Table from .arrow().
    This helper normalises both cases to a pa.Table.
    """
    if isinstance(arrow_result, pa.RecordBatchReader):
        return arrow_result.read_all()
    return arrow_result


class DuckDBEngine:
    """Wraps a single DuckDB connection with S3/Iceberg extensions configured.

    Thread safety: NOT thread-safe. Each instance is intended to be used by a
    single pipeline run in a single thread. The executor creates one DuckDBEngine
    per run in _phase2_build_result and closes it in the finally block of
    execute_pipeline. Do not share instances across threads.
    """

    def __init__(self, s3_config: S3Config, duckdb_config: DuckDBConfig | None = None) -> None:
        self._s3_config = s3_config
        self._duckdb_config = duckdb_config or DuckDBConfig()
        self._conn: duckdb.DuckDBPyConnection | None = None

    def _create_connection(self) -> duckdb.DuckDBPyConnection:
        # S3 setup is intentionally aligned with query/src/rat_query/engine.py.
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
        conn.execute("SET memory_limit = ?", [self._duckdb_config.memory_limit])
        conn.execute("SET threads = ?", [self._duckdb_config.threads])
        return conn

    @property
    def conn(self) -> duckdb.DuckDBPyConnection:
        if self._conn is None:
            self._conn = self._create_connection()
        return self._conn

    def query_arrow(self, sql: str) -> pa.Table:
        """Execute SQL and return result as a PyArrow Table."""
        result = self.conn.execute(sql)
        return _to_arrow_table(result.arrow())

    def execute(self, sql: str) -> None:
        """Execute SQL without returning results."""
        self.conn.execute(sql)

    def explain_analyze(self, sql: str) -> str:
        """Run EXPLAIN ANALYZE and return the plan text.

        Wraps the query in parentheses to handle multi-statement or complex SQL
        safely within the EXPLAIN ANALYZE prefix.
        """
        explain_sql = f"EXPLAIN ANALYZE ({sql})"
        result = self.conn.execute(explain_sql)
        rows = result.fetchall()
        return "\n".join(row[1] for row in rows)

    def get_memory_stats(self) -> dict[str, int]:
        """Return DuckDB memory usage from PRAGMA database_size."""
        result = self.conn.execute("CALL pragma_database_size()")
        rows = result.fetchall()
        if rows:
            # pragma_database_size returns:
            # (database_name, database_size, block_size, total_blocks,
            #  used_blocks, free_blocks, wal_size, memory_usage, memory_limit)
            desc = result.description
            col_names = [d[0] for d in desc] if desc else []
            row = rows[0]
            stats: dict[str, int] = {}
            for i, name in enumerate(col_names):
                if name in ("memory_usage", "memory_limit") and row[i] is not None:
                    stats[name] = self._parse_size(str(row[i]))
            return stats
        return {}

    @staticmethod
    def _parse_size(size_str: str) -> int:
        """Parse DuckDB human-readable size strings like '256.0 KiB' to bytes."""
        size_str = size_str.strip()
        multipliers = {
            "bytes": 1,
            "KiB": 1024,
            "MiB": 1024**2,
            "GiB": 1024**3,
            "TiB": 1024**4,
        }
        for suffix, mult in multipliers.items():
            if size_str.endswith(suffix):
                num = size_str[: -len(suffix)].strip()
                return int(float(num) * mult)
        try:
            return int(size_str)
        except ValueError:
            return 0

    def close(self) -> None:
        """Release the DuckDB connection."""
        if self._conn is not None:
            self._conn.close()
            self._conn = None
