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

    def test_s3_overrides_merged_into_config(self, s3_config: S3Config):
        """Per-run overrides win over env-level S3Config (ADR-018 precedence).

        ratd vends per-run credentials from the cloud plugin and ships them in
        the SubmitPipeline proto. The runner merges them via
        ``S3Config.with_overrides`` and passes the merged config to the
        DuckDBEngine — this test proves that DuckDB sees the override values,
        not the env-level defaults.
        """
        # The base config (from env / container) has long-lived MinIO creds.
        # The per-run overrides (from STS) carry session creds for a target bucket.
        overrides = {
            "access_key": "AKIA-OVR",
            "secret_key": "secret-ovr",
            "session_token": "sts-session-xyz",
            "region": "eu-west-3",
            "endpoint": "s3.eu-west-3.amazonaws.com",
            "bucket": "tenant-bucket",
            "use_ssl": "true",
        }
        merged = s3_config.with_overrides(overrides)

        # Sanity: the merge produced a new config with override values winning.
        assert merged.access_key == "AKIA-OVR"
        assert merged.secret_key == "secret-ovr"
        assert merged.session_token == "sts-session-xyz"
        assert merged.region == "eu-west-3"
        assert merged.endpoint == "s3.eu-west-3.amazonaws.com"
        assert merged.bucket == "tenant-bucket"
        assert merged.use_ssl is True
        # The base config is untouched (frozen dataclass — defensive check).
        assert s3_config.access_key == "test-access-key"

        with patch("rat_runner.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = DuckDBEngine(merged)
            _ = engine.conn

        # The DuckDB connection must be configured with the OVERRIDE values,
        # not the base config — proves the precedence is wired end-to-end.
        calls_str = "".join(str(c) for c in mock_conn.execute.call_args_list)
        assert "AKIA-OVR" in calls_str
        assert "secret-ovr" in calls_str
        assert "sts-session-xyz" in calls_str
        assert "eu-west-3" in calls_str
        assert "s3.eu-west-3.amazonaws.com" in calls_str
        # The env-level access key MUST NOT appear — overrides won.
        assert "test-access-key" not in calls_str

    def test_partial_overrides_keep_base_for_unset_fields(self, s3_config: S3Config):
        """Empty/unset override fields fall back to the env-level S3Config.

        The per-run cloud creds only set the bits the plugin owns (access key,
        secret, session token, region). Endpoint and bucket usually stay with
        the runner's env defaults so non-AWS deployments still resolve MinIO.
        """
        overrides = {
            "access_key": "AKIA-PARTIAL",
            "secret_key": "secret-partial",
            "session_token": "sts-token",
            "region": "us-east-2",
            # endpoint / bucket / use_ssl intentionally absent.
        }
        merged = s3_config.with_overrides(overrides)

        assert merged.access_key == "AKIA-PARTIAL"
        assert merged.session_token == "sts-token"
        # Fields not in the override map keep the base values.
        assert merged.endpoint == s3_config.endpoint
        assert merged.bucket == s3_config.bucket
        assert merged.use_ssl == s3_config.use_ssl
