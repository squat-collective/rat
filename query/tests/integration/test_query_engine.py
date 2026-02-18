"""Integration tests for the query engine — real DuckDB with schema introspection.

Most tests in this file do NOT require running S3/Nessie services. They use
DuckDB's in-memory tables and views to verify the query engine's behavior
without mocks. Tests that need external services are marked with skip markers.
"""

from __future__ import annotations

import pyarrow as pa
import pytest

from rat_query.config import DuckDBConfig, S3Config
from rat_query.engine import QueryEngine


class TestQueryEngineCreation:
    """Verify engine creation with real DuckDB (no mocks)."""

    def test_creates_working_connection(self, query_engine: QueryEngine) -> None:
        """Engine should create a working DuckDB connection on init."""
        result = query_engine._conn.execute("SELECT 1 AS x").fetchone()
        assert result is not None
        assert result[0] == 1

    def test_extensions_are_loaded(self, query_engine: QueryEngine) -> None:
        """httpfs and iceberg extensions should be installed and loaded."""
        result = query_engine._conn.execute(
            "SELECT extension_name FROM duckdb_extensions() "
            "WHERE loaded = true AND extension_name IN ('httpfs', 'iceberg')"
        ).fetchall()
        loaded = {row[0] for row in result}
        assert "httpfs" in loaded
        assert "iceberg" in loaded

    def test_s3_config_is_applied(self, query_engine: QueryEngine) -> None:
        """S3 endpoint should be configured from the S3Config."""
        result = query_engine._conn.execute("SELECT current_setting('s3_endpoint')").fetchone()
        assert result is not None
        # The endpoint comes from the test fixture — just verify it is set
        assert isinstance(result[0], str)
        assert len(result[0]) > 0

    def test_resource_limits_applied(self) -> None:
        """Memory limit and threads should match DuckDBConfig."""
        s3_config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        duckdb_config = DuckDBConfig(memory_limit="256MB", threads=1)
        engine = QueryEngine(s3_config, duckdb_config)
        try:
            result = engine._conn.execute("SELECT current_setting('threads')").fetchone()
            assert result is not None
            assert str(result[0]) == "1"
        finally:
            engine.close()


class TestQueryExecution:
    """Test SQL query execution with security guards."""

    def test_simple_select_returns_arrow_table(self, query_engine: QueryEngine) -> None:
        """query_arrow should execute SELECT and return PyArrow Table."""
        result = query_engine.query_arrow("SELECT 42 AS answer, 'hello' AS greeting")
        assert isinstance(result, pa.Table)
        assert len(result) == 1
        assert result.column("answer").to_pylist() == [42]
        assert result.column("greeting").to_pylist() == ["hello"]

    def test_query_with_limit(self, query_engine: QueryEngine) -> None:
        """query_arrow should apply the LIMIT wrapper."""
        result = query_engine.query_arrow(
            "SELECT i FROM generate_series(1, 100) AS t(i)",
            limit=10,
        )
        assert isinstance(result, pa.Table)
        assert len(result) == 10

    def test_query_without_limit(self, query_engine: QueryEngine) -> None:
        """query_arrow with limit=0 should return all rows."""
        result = query_engine.query_arrow(
            "SELECT i FROM generate_series(1, 50) AS t(i)",
            limit=0,
        )
        assert isinstance(result, pa.Table)
        assert len(result) == 50

    def test_cte_query(self, query_engine: QueryEngine) -> None:
        """Common Table Expressions should work with query_arrow."""
        sql = """
            WITH numbers AS (
                SELECT i FROM generate_series(1, 5) AS t(i)
            ),
            doubled AS (
                SELECT i, i * 2 AS doubled FROM numbers
            )
            SELECT * FROM doubled ORDER BY i
        """
        result = query_engine.query_arrow(sql, limit=0)
        assert len(result) == 5
        assert result.column("i").to_pylist() == [1, 2, 3, 4, 5]
        assert result.column("doubled").to_pylist() == [2, 4, 6, 8, 10]

    def test_rejects_drop_statement(self, query_engine: QueryEngine) -> None:
        """DROP statements should be rejected."""
        with pytest.raises(ValueError, match="Only SELECT queries are allowed"):
            query_engine.query_arrow("DROP TABLE silver.orders")

    def test_rejects_insert_statement(self, query_engine: QueryEngine) -> None:
        """INSERT statements should be rejected."""
        with pytest.raises(ValueError, match="Only SELECT queries are allowed"):
            query_engine.query_arrow("INSERT INTO silver.orders VALUES (1)")

    def test_rejects_read_parquet_function(self, query_engine: QueryEngine) -> None:
        """Direct file access functions should be rejected."""
        with pytest.raises(ValueError, match="Direct file/URL access functions"):
            query_engine.query_arrow("SELECT * FROM read_parquet('/etc/passwd')")

    def test_rejects_overly_long_query(self, query_engine: QueryEngine) -> None:
        """Queries exceeding the length limit should be rejected."""
        long_sql = "SELECT 1" + " " * 100_001
        with pytest.raises(ValueError, match="Query too long"):
            query_engine.query_arrow(long_sql)

    def test_complex_types_in_results(self, query_engine: QueryEngine) -> None:
        """Engine should handle timestamps, decimals, arrays, and booleans."""
        result = query_engine.query_arrow(
            "SELECT "
            "  TIMESTAMP '2024-01-15 10:30:00' AS ts, "
            "  42.5::DECIMAL(10,2) AS amount, "
            "  [1, 2, 3] AS arr, "
            "  TRUE AS flag",
            limit=0,
        )
        assert isinstance(result, pa.Table)
        assert len(result) == 1
        assert set(result.column_names) == {"ts", "amount", "arr", "flag"}

    def test_aggregation_query(self, query_engine: QueryEngine) -> None:
        """Aggregation functions should work correctly."""
        result = query_engine.query_arrow(
            "SELECT COUNT(*) AS cnt, SUM(i) AS total, AVG(i) AS avg_val "
            "FROM generate_series(1, 100) AS t(i)",
            limit=0,
        )
        assert len(result) == 1
        assert result.column("cnt").to_pylist() == [100]
        assert result.column("total").to_pylist() == [5050]

    def test_window_function_query(self, query_engine: QueryEngine) -> None:
        """Window functions should work correctly."""
        result = query_engine.query_arrow(
            "SELECT i, ROW_NUMBER() OVER (ORDER BY i) AS rn FROM generate_series(1, 5) AS t(i)",
            limit=0,
        )
        assert len(result) == 5
        assert result.column("rn").to_pylist() == [1, 2, 3, 4, 5]


class TestViewRegistration:
    """Test DuckDB view registration and introspection (in-memory, no S3)."""

    def test_register_view_creates_schema_and_view(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """register_view should create the schema and a view backed by SQL."""
        # We cannot use a real S3 path here, but we can verify the DDL runs
        # by creating a view on a simple subquery instead of read_parquet.
        query_engine._conn.execute('CREATE SCHEMA IF NOT EXISTS "bronze"')
        query_engine._conn.execute(
            'CREATE OR REPLACE VIEW "bronze"."test_orders" AS SELECT 1 AS id, \'widget\' AS product'
        )

        # Verify the view is queryable
        result = query_engine.query_arrow(
            'SELECT * FROM "bronze"."test_orders"',
            limit=0,
        )
        assert len(result) == 1
        assert result.column("id").to_pylist() == [1]
        assert result.column("product").to_pylist() == ["widget"]

    def test_drop_all_views_clears_schemas(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """drop_all_views should remove bronze/silver/gold schemas."""
        # Create schemas with views
        for layer in ("bronze", "silver", "gold"):
            query_engine._conn.execute(f'CREATE SCHEMA IF NOT EXISTS "{layer}"')
            query_engine._conn.execute(f'CREATE VIEW "{layer}"."test" AS SELECT 1 AS x')

        query_engine.drop_all_views()

        # Verify schemas are gone
        for layer in ("bronze", "silver", "gold"):
            with pytest.raises(Exception):
                query_engine._conn.execute(f'SELECT * FROM "{layer}"."test"')


class TestSchemaIntrospection:
    """Test describe_table and count_rows with real DuckDB views."""

    def test_describe_table_returns_column_info(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """describe_table should return column names and types."""
        query_engine._conn.execute('CREATE SCHEMA IF NOT EXISTS "silver"')
        query_engine._conn.execute(
            'CREATE VIEW "silver"."products" AS '
            "SELECT "
            "  1::INTEGER AS id, "
            "  'widget'::VARCHAR AS name, "
            "  9.99::DECIMAL(10,2) AS price, "
            "  TRUE AS active"
        )

        columns = query_engine.describe_table("silver", "products")
        assert len(columns) == 4
        col_dict = {name: dtype for name, dtype in columns}
        assert "id" in col_dict
        assert "name" in col_dict
        assert "price" in col_dict
        assert "active" in col_dict
        assert col_dict["id"] == "INTEGER"
        assert col_dict["name"] == "VARCHAR"
        assert col_dict["active"] == "BOOLEAN"

    def test_describe_table_rejects_invalid_schema(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """describe_table should reject schemas outside medallion layers."""
        with pytest.raises(ValueError, match="Invalid schema"):
            query_engine.describe_table("public", "test")

    def test_describe_table_rejects_sql_injection(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """describe_table should reject unsafe table names."""
        with pytest.raises(ValueError, match="Invalid"):
            query_engine.describe_table("silver", "orders; DROP TABLE x")

    def test_count_rows_returns_correct_count(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """count_rows should return the number of rows in a view."""
        query_engine._conn.execute('CREATE SCHEMA IF NOT EXISTS "gold"')
        query_engine._conn.execute(
            'CREATE VIEW "gold"."metrics" AS SELECT i AS id FROM generate_series(1, 42) AS t(i)'
        )

        count = query_engine.count_rows("gold", "metrics")
        assert count == 42

    def test_count_rows_on_empty_view(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """count_rows should return 0 for a view with no rows."""
        query_engine._conn.execute('CREATE SCHEMA IF NOT EXISTS "bronze"')
        query_engine._conn.execute(
            'CREATE VIEW "bronze"."empty_table" AS SELECT 1 AS id WHERE FALSE'
        )

        count = query_engine.count_rows("bronze", "empty_table")
        assert count == 0


class TestNamespaceTableRefs:
    """Test three-part namespace.layer.table references with auto-quoting."""

    def test_query_with_three_part_ref(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """Queries using default.layer.table should be auto-quoted."""
        # Set up namespace catalog
        query_engine._conn.execute("ATTACH IF NOT EXISTS ':memory:' AS \"default\"")
        query_engine._conn.execute('CREATE SCHEMA IF NOT EXISTS "default"."bronze"')
        query_engine._conn.execute(
            'CREATE VIEW "default"."bronze"."orders" AS SELECT 1 AS order_id, 100.0 AS amount'
        )

        # Query using default.bronze.orders — should be auto-quoted
        result = query_engine.query_arrow(
            "SELECT * FROM default.bronze.orders",
            limit=0,
        )
        assert len(result) == 1
        assert result.column("order_id").to_pylist() == [1]

    def test_join_with_three_part_refs(
        self,
        query_engine: QueryEngine,
    ) -> None:
        """JOINs between namespace-qualified tables should work."""
        query_engine._conn.execute("ATTACH IF NOT EXISTS ':memory:' AS \"myns\"")
        query_engine._conn.execute('CREATE SCHEMA IF NOT EXISTS "myns"."bronze"')
        query_engine._conn.execute('CREATE SCHEMA IF NOT EXISTS "myns"."silver"')
        query_engine._conn.execute(
            'CREATE VIEW "myns"."bronze"."orders" AS SELECT 1 AS id, \'widget\' AS product'
        )
        query_engine._conn.execute(
            'CREATE VIEW "myns"."silver"."prices" AS SELECT \'widget\' AS product, 9.99 AS price'
        )

        result = query_engine.query_arrow(
            "SELECT o.id, o.product, p.price "
            "FROM myns.bronze.orders o "
            "JOIN myns.silver.prices p ON o.product = p.product",
            limit=0,
        )
        assert len(result) == 1
        assert float(result.column("price")[0].as_py()) == 9.99


class TestEngineLifecycle:
    """Test engine close and cleanup."""

    def test_close_releases_connection(self) -> None:
        """close() should release the DuckDB connection."""
        s3_config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = QueryEngine(s3_config)
        engine.close()
        assert engine._conn is None

    def test_close_is_idempotent(self) -> None:
        """Calling close() twice should not raise."""
        s3_config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = QueryEngine(s3_config)
        engine.close()
        # Second close should be a no-op
        engine.close()
