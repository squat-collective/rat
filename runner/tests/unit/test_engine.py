"""Tests for engine — DuckDB engine with S3 extensions."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pyarrow as pa

from rat_runner.config import S3Config
from rat_runner.engine import DuckDBEngine


class TestDuckDBEngine:
    def test_creates_connection_lazily(self, s3_config: S3Config):
        engine = DuckDBEngine(s3_config)
        assert engine._conn is None

        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            _ = engine.conn

        mock_connect.assert_called_once_with(":memory:")
        assert engine._conn is mock_conn

    def test_configures_s3_extensions(self, s3_config: S3Config):
        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = DuckDBEngine(s3_config)
            _ = engine.conn

        # Should have called execute for httpfs, iceberg, and S3 config
        calls = mock_conn.execute.call_args_list
        assert any("httpfs" in str(c) for c in calls)
        assert any("iceberg" in str(c) for c in calls)
        assert any("s3_endpoint" in str(c) for c in calls)

    def test_close_releases_connection(self, s3_config: S3Config):
        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = DuckDBEngine(s3_config)
            _ = engine.conn
            engine.close()

        mock_conn.close.assert_called_once()
        assert engine._conn is None

    def test_close_noop_when_not_connected(self, s3_config: S3Config):
        engine = DuckDBEngine(s3_config)
        engine.close()  # should not raise

    def test_query_arrow_handles_record_batch_reader(self, s3_config: S3Config):
        table = pa.table({"x": [1, 2, 3]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = DuckDBEngine(s3_config)
            result = engine.query_arrow("SELECT 1")

        assert isinstance(result, pa.Table)
        assert len(result) == 3

    def test_sets_session_token_when_present(self):
        config = S3Config(
            endpoint="minio:9000",
            access_key="ak",
            secret_key="sk",
            bucket="test",
            session_token="sts-token-123",
        )
        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = DuckDBEngine(config)
            _ = engine.conn

        calls = mock_conn.execute.call_args_list
        session_calls = [c for c in calls if "s3_session_token" in str(c)]
        assert len(session_calls) == 1
        assert "sts-token-123" in str(session_calls[0])

    def test_explain_analyze_wraps_sql_in_parens(self, s3_config: S3Config):
        """explain_analyze wraps query in parentheses for safe EXPLAIN ANALYZE."""
        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.fetchall.return_value = [
                (None, "Physical Plan"),
                (None, "  Scan: table"),
            ]
            mock_conn.execute.return_value = mock_result

            engine = DuckDBEngine(s3_config)
            result = engine.explain_analyze("SELECT * FROM t")

        # Verify the SQL passed to execute is wrapped in parens
        exec_calls = mock_conn.execute.call_args_list
        explain_calls = [c for c in exec_calls if "EXPLAIN ANALYZE" in str(c)]
        assert len(explain_calls) == 1
        assert "EXPLAIN ANALYZE (SELECT * FROM t)" in str(explain_calls[0])
        assert result == "Physical Plan\n  Scan: table"

    def test_skips_session_token_when_empty(self, s3_config: S3Config):
        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = DuckDBEngine(s3_config)
            _ = engine.conn

        calls = mock_conn.execute.call_args_list
        session_calls = [c for c in calls if "s3_session_token" in str(c)]
        assert len(session_calls) == 0
