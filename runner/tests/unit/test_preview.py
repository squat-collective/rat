"""Tests for the preview pipeline executor."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pyarrow as pa
import pytest

from rat_runner.config import NessieConfig, S3Config
from rat_runner.preview import (
    _extract_columns,
    preview_pipeline,
)

_MOD = "rat_runner.preview"


@pytest.fixture
def s3_config() -> S3Config:
    return S3Config(
        endpoint="localhost:9000",
        access_key="test-access-key",
        secret_key="test-secret-key",
        bucket="test-bucket",
        use_ssl=False,
    )


@pytest.fixture
def nessie_config() -> NessieConfig:
    return NessieConfig(url="http://localhost:19120/api/v1")


class TestExtractColumns:
    def test_extracts_names_and_types(self):
        table = pa.table({"id": [1, 2], "name": ["a", "b"]})
        cols = _extract_columns(table)
        assert len(cols) == 2
        assert cols[0].name == "id"
        assert cols[0].type == "int64"
        assert cols[1].name == "name"
        assert cols[1].type == "string"


class TestPreviewSQL:
    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_sql_preview_returns_limited_rows(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        # Arrange: SQL pipeline found
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine.conn.execute.return_value.fetchone.return_value = (42,)
        mock_engine.explain_analyze.return_value = "EXPLAIN: scan"
        mock_engine.get_memory_stats.return_value = {"memory_usage": 1024}

        # Act
        with patch(f"{_MOD}.compile_sql", return_value="SELECT 1 AS id"):
            result = preview_pipeline(
                namespace="default",
                layer="silver",
                pipeline_name="orders",
                s3_config=s3_config,
                nessie_config=nessie_config,
                preview_limit=10,
            )

        # Assert
        assert result.error == ""
        assert result.arrow_table is not None
        assert result.arrow_table.num_rows == 1
        # When returned rows < preview_limit, total_row_count equals num_rows
        # (no separate COUNT(*) query needed — we already have the exact total)
        assert result.total_row_count == 1
        assert result.explain_output == "EXPLAIN: scan"
        assert result.memory_peak_bytes == 1024
        assert len(result.columns) == 1
        assert result.columns[0].name == "id"

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_sql_preview_captures_phase_timings(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine.conn.execute.return_value.fetchone.return_value = (1,)
        mock_engine.explain_analyze.return_value = ""
        mock_engine.get_memory_stats.return_value = {}

        with patch(f"{_MOD}.compile_sql", return_value="SELECT 1 AS id"):
            result = preview_pipeline(
                namespace="default",
                layer="silver",
                pipeline_name="orders",
                s3_config=s3_config,
                nessie_config=nessie_config,
            )

        phase_names = [p.name for p in result.phases]
        assert phase_names == ["detect", "compile", "execute", "explain", "count"]
        for p in result.phases:
            assert p.duration_ms >= 0

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_sql_preview_explain_failure_produces_warning(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine.conn.execute.return_value.fetchone.return_value = (1,)
        mock_engine.explain_analyze.side_effect = Exception("EXPLAIN not supported")
        mock_engine.get_memory_stats.return_value = {}

        with patch(f"{_MOD}.compile_sql", return_value="SELECT 1 AS id"):
            result = preview_pipeline(
                namespace="default",
                layer="silver",
                pipeline_name="orders",
                s3_config=s3_config,
                nessie_config=nessie_config,
            )

        assert result.error == ""
        assert any("EXPLAIN ANALYZE failed" in w for w in result.warnings)


class TestPreviewPython:
    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    @patch(f"{_MOD}.execute_python_pipeline")
    def test_python_preview_slices_result(
        self, mock_exec, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        # Python pipeline returns 200 rows, but limit is 50
        mock_read.side_effect = lambda cfg, key: (
            "result = pa.table({'id': range(200)})" if key.endswith(".py") else None
        )
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_exec.return_value = pa.table({"id": list(range(200))})
        mock_engine.get_memory_stats.return_value = {}

        result = preview_pipeline(
            namespace="default",
            layer="silver",
            pipeline_name="transform",
            s3_config=s3_config,
            nessie_config=nessie_config,
            preview_limit=50,
        )

        assert result.error == ""
        assert result.arrow_table is not None
        assert result.arrow_table.num_rows == 50
        assert result.total_row_count == 200

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    @patch(f"{_MOD}.execute_python_pipeline")
    def test_python_preview_injects_logger(
        self, mock_exec, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        mock_read.side_effect = lambda cfg, key: (
            "log.info('hello')\nresult = pa.table({'x': [1]})" if key.endswith(".py") else None
        )
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_exec.return_value = pa.table({"x": [1]})
        mock_engine.get_memory_stats.return_value = {}

        preview_pipeline(
            namespace="default",
            layer="silver",
            pipeline_name="transform",
            s3_config=s3_config,
            nessie_config=nessie_config,
        )

        # Verify logger was passed to execute_python_pipeline
        call_kwargs = mock_exec.call_args
        assert call_kwargs.kwargs.get("logger") is not None or (
            len(call_kwargs.args) > 9 and call_kwargs.args[9] is not None
        )


class TestPreviewLandingZoneFn:
    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_sql_preview_passes_landing_zone_fn(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        """Verify compile_sql is called with landing_zone_fn during SQL preview."""
        mock_read.side_effect = lambda cfg, key: (
            "SELECT * FROM {{ landing_zone('orders') }}" if key.endswith(".sql") else None
        )
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine.conn.execute.return_value.fetchone.return_value = (1,)
        mock_engine.explain_analyze.return_value = ""
        mock_engine.get_memory_stats.return_value = {}

        with patch(f"{_MOD}.compile_sql", return_value="SELECT 1") as mock_compile:
            result = preview_pipeline(
                namespace="default",
                layer="bronze",
                pipeline_name="ingest",
                s3_config=s3_config,
                nessie_config=nessie_config,
            )

        assert result.error == ""
        call_kwargs = mock_compile.call_args
        assert call_kwargs.kwargs.get("landing_zone_fn") is not None

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    @patch(f"{_MOD}.execute_python_pipeline")
    def test_python_preview_passes_landing_zone_fn(
        self, mock_exec, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        """Verify execute_python_pipeline is called with landing_zone_fn during Python preview."""
        mock_read.side_effect = lambda cfg, key: (
            "result = pa.table({'id': [1]})" if key.endswith(".py") else None
        )
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_exec.return_value = pa.table({"id": [1]})
        mock_engine.get_memory_stats.return_value = {}

        result = preview_pipeline(
            namespace="default",
            layer="bronze",
            pipeline_name="ingest",
            s3_config=s3_config,
            nessie_config=nessie_config,
        )

        assert result.error == ""
        call_kwargs = mock_exec.call_args
        assert call_kwargs.kwargs.get("landing_zone_fn") is not None


class TestPreviewInlineCode:
    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_preview_uses_inline_code_when_provided(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        """When code is provided, S3 reads for source file should be skipped."""
        mock_read.return_value = None  # config.yaml not found
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine.conn.execute.return_value.fetchone.return_value = (1,)
        mock_engine.explain_analyze.return_value = ""
        mock_engine.get_memory_stats.return_value = {}

        with patch(f"{_MOD}.compile_sql", return_value="SELECT 1 AS x"):
            result = preview_pipeline(
                namespace="default",
                layer="silver",
                pipeline_name="orders",
                s3_config=s3_config,
                nessie_config=nessie_config,
                preview_limit=10,
                code="SELECT 1 AS x",
                pipeline_type="sql",
            )

        assert result.error == ""
        assert result.arrow_table is not None
        # read_s3_text should NOT be called for pipeline.sql / pipeline.py
        # It may still be called for config.yaml via _load_config
        source_calls = [
            c
            for c in mock_read.call_args_list
            if c.args[1].endswith(".sql") or c.args[1].endswith(".py")
        ]
        assert len(source_calls) == 0

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_preview_reads_s3_when_code_is_none(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        """When code is None, should fall back to S3 reads (backward compat)."""
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine.conn.execute.return_value.fetchone.return_value = (1,)
        mock_engine.explain_analyze.return_value = ""
        mock_engine.get_memory_stats.return_value = {}

        with patch(f"{_MOD}.compile_sql", return_value="SELECT 1 AS id"):
            result = preview_pipeline(
                namespace="default",
                layer="silver",
                pipeline_name="orders",
                s3_config=s3_config,
                nessie_config=nessie_config,
                preview_limit=10,
                code=None,
            )

        assert result.error == ""
        # read_s3_text should have been called for source file
        source_calls = [
            c
            for c in mock_read.call_args_list
            if c.args[1].endswith(".sql") or c.args[1].endswith(".py")
        ]
        assert len(source_calls) > 0

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    @patch(f"{_MOD}.execute_python_pipeline")
    def test_preview_uses_inline_python_code(
        self, mock_exec, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        """When code+pipeline_type='python' is provided, uses Python path."""
        mock_read.return_value = None  # config.yaml not found
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_exec.return_value = pa.table({"y": [42]})
        mock_engine.get_memory_stats.return_value = {}

        result = preview_pipeline(
            namespace="default",
            layer="silver",
            pipeline_name="transform",
            s3_config=s3_config,
            nessie_config=nessie_config,
            code="result = pa.table({'y': [42]})",
            pipeline_type="python",
        )

        assert result.error == ""
        assert result.arrow_table is not None
        assert result.arrow_table.num_rows == 1
        # Should not read any source files from S3
        source_calls = [
            c
            for c in mock_read.call_args_list
            if c.args[1].endswith(".sql") or c.args[1].endswith(".py")
        ]
        assert len(source_calls) == 0


class TestPreviewErrors:
    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_missing_pipeline_returns_error(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        mock_read.return_value = None
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine

        result = preview_pipeline(
            namespace="default",
            layer="silver",
            pipeline_name="nonexistent",
            s3_config=s3_config,
            nessie_config=nessie_config,
        )

        assert "No pipeline.py or pipeline.sql found" in result.error

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_sql_execution_error_returned(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        mock_read.side_effect = lambda cfg, key: (
            "SELECT * FROM nonexistent_table" if key.endswith(".sql") else None
        )
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine
        mock_engine.query_arrow.side_effect = Exception("Table nonexistent_table not found")
        mock_engine.get_memory_stats.return_value = {}

        with patch(f"{_MOD}.compile_sql", return_value="SELECT * FROM nonexistent_table"):
            result = preview_pipeline(
                namespace="default",
                layer="silver",
                pipeline_name="broken",
                s3_config=s3_config,
                nessie_config=nessie_config,
            )

        assert "nonexistent_table" in result.error

    @patch(f"{_MOD}.DuckDBEngine")
    @patch(f"{_MOD}.read_s3_text")
    def test_preview_always_collects_logs(
        self, mock_read, mock_engine_cls, s3_config, nessie_config
    ):
        mock_read.return_value = None
        mock_engine = MagicMock()
        mock_engine_cls.return_value = mock_engine

        result = preview_pipeline(
            namespace="default",
            layer="silver",
            pipeline_name="broken",
            s3_config=s3_config,
            nessie_config=nessie_config,
        )

        # Even on error, logs should be captured
        assert len(result.logs) > 0
        assert any("Preview failed" in log.message for log in result.logs)
