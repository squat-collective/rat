"""Integration tests for DuckDB engine — real DuckDB with S3 extensions.

These tests verify that the DuckDB engine can be created with S3 extensions
loaded and configured, and that basic SQL operations work correctly.
They do NOT require a running S3 service for most tests — only that DuckDB
can install and load the httpfs/iceberg extensions.
"""

from __future__ import annotations

import pyarrow as pa

from rat_runner.config import DuckDBConfig, S3Config
from rat_runner.engine import DuckDBEngine


class TestDuckDBEngineCreation:
    """Verify engine creation with real DuckDB (no mocks)."""

    def test_creates_in_memory_connection(self) -> None:
        """Engine should create a working in-memory DuckDB connection."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            result = engine.conn.execute("SELECT 1 AS x").fetchone()
            assert result is not None
            assert result[0] == 1
        finally:
            engine.close()

    def test_extensions_are_loaded(self) -> None:
        """httpfs and iceberg extensions should be installed and loaded."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            # Query loaded extensions to verify httpfs is present
            result = engine.conn.execute(
                "SELECT extension_name FROM duckdb_extensions() "
                "WHERE loaded = true AND extension_name IN ('httpfs', 'iceberg')"
            ).fetchall()
            loaded = {row[0] for row in result}
            assert "httpfs" in loaded, "httpfs extension should be loaded"
            assert "iceberg" in loaded, "iceberg extension should be loaded"
        finally:
            engine.close()

    def test_s3_configuration_is_applied(self) -> None:
        """S3 settings should be configured on the connection."""
        config = S3Config(
            endpoint="my-minio:9000",
            access_key="my-access-key",
            secret_key="my-secret-key",
            bucket="my-bucket",
            region="eu-west-1",
        )
        engine = DuckDBEngine(config)
        try:
            result = engine.conn.execute("SELECT current_setting('s3_endpoint')").fetchone()
            assert result is not None
            assert result[0] == "my-minio:9000"

            result = engine.conn.execute("SELECT current_setting('s3_region')").fetchone()
            assert result is not None
            assert result[0] == "eu-west-1"
        finally:
            engine.close()

    def test_duckdb_config_limits_applied(self) -> None:
        """Memory limit and thread count from DuckDBConfig should be applied."""
        s3_config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        duckdb_config = DuckDBConfig(memory_limit="256MB", threads=1)
        engine = DuckDBEngine(s3_config, duckdb_config)
        try:
            result = engine.conn.execute("SELECT current_setting('threads')").fetchone()
            assert result is not None
            assert str(result[0]) == "1"
        finally:
            engine.close()

    def test_session_token_is_set_when_provided(self) -> None:
        """When session_token is set, s3_session_token should be configured."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
            session_token="my-sts-token",
        )
        engine = DuckDBEngine(config)
        try:
            result = engine.conn.execute("SELECT current_setting('s3_session_token')").fetchone()
            assert result is not None
            assert result[0] == "my-sts-token"
        finally:
            engine.close()


class TestDuckDBEngineOperations:
    """Verify SQL operations with real DuckDB."""

    def test_query_arrow_returns_pyarrow_table(self) -> None:
        """query_arrow should return a PyArrow Table from SQL results."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            result = engine.query_arrow(
                "SELECT 1 AS id, 'hello' AS name UNION ALL SELECT 2, 'world'"
            )
            assert isinstance(result, pa.Table)
            assert len(result) == 2
            assert result.column_names == ["id", "name"]
            assert result.column("id").to_pylist() == [1, 2]
            assert result.column("name").to_pylist() == ["hello", "world"]
        finally:
            engine.close()

    def test_execute_runs_ddl(self) -> None:
        """execute() should run DDL statements without returning results."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            engine.execute("CREATE TABLE test_table (id INTEGER, name VARCHAR)")
            engine.execute("INSERT INTO test_table VALUES (1, 'a'), (2, 'b')")

            result = engine.query_arrow("SELECT * FROM test_table ORDER BY id")
            assert len(result) == 2
            assert result.column("id").to_pylist() == [1, 2]
        finally:
            engine.close()

    def test_query_arrow_with_complex_types(self) -> None:
        """Engine should handle DuckDB-native types (timestamps, decimals, etc.)."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            result = engine.query_arrow(
                "SELECT "
                "  TIMESTAMP '2024-01-15 10:30:00' AS ts, "
                "  42.5::DECIMAL(10,2) AS amount, "
                "  [1, 2, 3] AS arr, "
                "  TRUE AS flag"
            )
            assert isinstance(result, pa.Table)
            assert len(result) == 1
            assert "ts" in result.column_names
            assert "amount" in result.column_names
            assert "arr" in result.column_names
            assert "flag" in result.column_names
        finally:
            engine.close()

    def test_explain_analyze_returns_plan(self) -> None:
        """explain_analyze() should return a query plan string."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            engine.execute("CREATE TABLE explain_test (x INTEGER)")
            engine.execute("INSERT INTO explain_test VALUES (1), (2), (3)")

            plan = engine.explain_analyze("SELECT * FROM explain_test")
            assert isinstance(plan, str)
            assert len(plan) > 0
        finally:
            engine.close()

    def test_get_memory_stats_returns_dict(self) -> None:
        """get_memory_stats() should return memory usage information."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            stats = engine.get_memory_stats()
            assert isinstance(stats, dict)
            # memory_limit should be present and reflect configured limit
            assert "memory_limit" in stats
        finally:
            engine.close()

    def test_close_prevents_further_queries(self) -> None:
        """After close(), accessing conn should create a new connection."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        # Access conn to create initial connection
        engine.conn.execute("SELECT 1")
        engine.close()
        assert engine._conn is None

        # Accessing conn again should create a fresh connection
        result = engine.conn.execute("SELECT 42").fetchone()
        assert result is not None
        assert result[0] == 42
        engine.close()

    def test_write_and_read_pyarrow_roundtrip(self) -> None:
        """Verify data roundtrip: PyArrow Table -> DuckDB -> PyArrow Table."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            # Create a PyArrow table
            original = pa.table(
                {
                    "id": pa.array([1, 2, 3], type=pa.int64()),
                    "name": pa.array(["alice", "bob", "charlie"], type=pa.string()),
                    "score": pa.array([95.5, 87.3, 91.0], type=pa.float64()),
                }
            )

            # Register and query through DuckDB
            engine.conn.register("source", original)
            result = engine.query_arrow("SELECT * FROM source ORDER BY id")

            assert len(result) == 3
            assert result.column("id").to_pylist() == [1, 2, 3]
            assert result.column("name").to_pylist() == ["alice", "bob", "charlie"]
            assert result.column("score").to_pylist() == [95.5, 87.3, 91.0]
        finally:
            engine.close()

    def test_large_dataset_query(self) -> None:
        """Engine should handle moderately large datasets without issues."""
        config = S3Config(
            endpoint="localhost:9000",
            access_key="test",
            secret_key="test",
            bucket="test",
        )
        engine = DuckDBEngine(config)
        try:
            # Generate 10k rows using DuckDB's generate_series
            result = engine.query_arrow(
                "SELECT i AS id, 'row_' || i AS name, random() AS value "
                "FROM generate_series(1, 10000) AS t(i)"
            )
            assert isinstance(result, pa.Table)
            assert len(result) == 10000
        finally:
            engine.close()
