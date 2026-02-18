"""Tests for executor — pipeline execution core loop with all 6 features."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pyarrow as pa

from rat_runner.config import NessieConfig, S3Config
from rat_runner.executor import execute_pipeline
from rat_runner.models import PipelineConfig, QualityTestResult, RunState, RunStatus


def _make_run(**kwargs) -> RunState:
    defaults = {
        "run_id": "r1",
        "namespace": "myns",
        "layer": "silver",
        "pipeline_name": "orders",
        "trigger": "manual",
    }
    defaults.update(kwargs)
    return RunState(**defaults)


# Common patches for the executor's imports
_EXEC_PREFIX = "rat_runner.executor"


def _patch_all():
    """Return patches for all executor dependencies."""
    return {
        "read_s3_text": patch(f"{_EXEC_PREFIX}.read_s3_text"),
        "create_branch": patch(f"{_EXEC_PREFIX}.create_branch"),
        "merge_branch": patch(f"{_EXEC_PREFIX}.merge_branch"),
        "delete_branch": patch(f"{_EXEC_PREFIX}.delete_branch"),
        "write_iceberg": patch(f"{_EXEC_PREFIX}.write_iceberg"),
        "merge_iceberg": patch(f"{_EXEC_PREFIX}.merge_iceberg"),
        "append_iceberg": patch(f"{_EXEC_PREFIX}.append_iceberg"),
        "delete_insert_iceberg": patch(f"{_EXEC_PREFIX}.delete_insert_iceberg"),
        "scd2_iceberg": patch(f"{_EXEC_PREFIX}.scd2_iceberg"),
        "snapshot_iceberg": patch(f"{_EXEC_PREFIX}.snapshot_iceberg"),
        "read_watermark": patch(f"{_EXEC_PREFIX}.read_watermark"),
        "DuckDBEngine": patch(f"{_EXEC_PREFIX}.DuckDBEngine"),
        "run_quality_tests": patch(f"{_EXEC_PREFIX}.run_quality_tests"),
        "has_error_failures": patch(f"{_EXEC_PREFIX}.has_error_failures"),
        "execute_python_pipeline": patch(f"{_EXEC_PREFIX}.execute_python_pipeline"),
    }


class TestExecutePipelineSQLBasic:
    """Tests for backward-compatible SQL pipeline execution."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_reads_sql_from_s3(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        # Verify S3 key for SQL (first tries .py, then .sql)
        sql_key = "myns/pipelines/silver/orders/pipeline.sql"
        mock_read.assert_any_call(s3_config, sql_key)

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_compiles_and_executes_sql(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: (
            "SELECT 42 AS value" if key.endswith(".sql") else None
        )
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"value": [42]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_engine.query_arrow.assert_called_once()
        sql_arg = mock_engine.query_arrow.call_args[0][0]
        assert "42" in sql_arg

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_writes_to_iceberg(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        data = pa.table({"id": [1, 2, 3]})
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = data
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 3

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_write.assert_called_once()
        args = mock_write.call_args
        assert args[0][0] is data  # data argument
        assert args[0][1] == "myns.silver.orders"  # table_name

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_sets_success_on_completion(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.SUCCESS
        assert run.rows_written == 1
        assert run.duration_ms >= 0

    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_sets_failed_on_error(
        self,
        mock_read: MagicMock,
        mock_create: MagicMock,
        mock_delete: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = Exception("S3 unreachable")

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "S3 unreachable" in run.error
        assert run.duration_ms >= 0

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_respects_cancellation(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None

        run = _make_run()
        run.cancel_event.set()  # pre-cancel

        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.CANCELLED
        assert "cancelled" in run.error.lower()

    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_handles_missing_sql_file(
        self,
        mock_read: MagicMock,
        mock_create: MagicMock,
        mock_delete: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.return_value = None  # no .py or .sql file

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "not found" in run.error.lower()

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_zero_rows_skips_write(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: (
            "SELECT 1 WHERE false" if key.endswith(".sql") else None
        )
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": pa.array([], type=pa.int64())})
        mock_engine_cls.return_value = mock_engine

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.SUCCESS
        assert run.rows_written == 0
        mock_write.assert_not_called()

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_records_duration_ms(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.duration_ms >= 0


class TestExecutePipelinePython:
    """Tests for Python pipeline detection and execution."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.execute_python_pipeline")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_detects_python_pipeline(
        self,
        mock_read: MagicMock,
        mock_py_exec: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # .py exists → Python pipeline
        def read_side(cfg, key):
            if key.endswith(".py"):
                return "result = pa.table({'x': [1]})"
            if key.endswith(".yaml"):
                return None
            return None  # .sql not read when .py exists

        mock_read.side_effect = read_side
        mock_py_exec.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = MagicMock()
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_py_exec.assert_called_once()
        assert run.status == RunStatus.SUCCESS


class TestExecutePipelineIncremental:
    """Tests for incremental merge path."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.merge_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.parse_pipeline_config")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_uses_merge_for_incremental(
        self,
        mock_read: MagicMock,
        mock_parse_config: MagicMock,
        mock_engine_cls: MagicMock,
        mock_merge_ice: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):

        def read_side(cfg, key):
            if key.endswith(".py"):
                return None
            if key.endswith(".sql"):
                return "SELECT 1 AS id"
            if key.endswith(".yaml"):
                return "merge_strategy: incremental"
            return None

        mock_read.side_effect = read_side
        mock_parse_config.return_value = PipelineConfig(
            merge_strategy="incremental",
            unique_key=("id",),
        )

        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_merge_ice.return_value = 5

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_merge_ice.assert_called_once()
        assert run.rows_written == 5


class TestExecutePipelineBranches:
    """Tests for ephemeral Nessie branch lifecycle."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_success_merges_branch(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.SUCCESS
        mock_create.assert_called_once()
        mock_merge.assert_called_once()

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=True)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests")
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_quality_failure_deletes_branch(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1
        mock_quality.return_value = [
            QualityTestResult(
                test_name="t1",
                test_file="f1",
                severity="error",
                status="fail",
                row_count=3,
            )
        ]

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "quality" in run.error.lower()
        mock_merge.assert_not_called()  # branch NOT merged

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", side_effect=Exception("Nessie down"))
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_branch_creation_failure_falls_back_to_main(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        # Falls back to main — still succeeds
        assert run.status == RunStatus.SUCCESS
        assert run.branch == "main"
        mock_merge.assert_not_called()  # no branch to merge


class TestExecutePipelineArchiveLandingZones:
    """Tests for archive_landing_zones annotation."""

    @patch(f"{_EXEC_PREFIX}.move_s3_keys")
    @patch(f"{_EXEC_PREFIX}.list_s3_keys")
    @patch(f"{_EXEC_PREFIX}.validate_landing_zones", return_value=[])
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_archives_files_after_successful_merge(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_validate_lz: MagicMock,
        mock_list_keys: MagicMock,
        mock_move_keys: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # SQL with landing_zone + archive annotation
        sql = """-- @archive_landing_zones: true
SELECT * FROM read_csv_auto('{{ landing_zone('orders') }}/*.csv')"""

        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        landing_keys = [
            "myns/landing/orders/file1.csv",
            "myns/landing/orders/file2.csv",
        ]
        mock_list_keys.return_value = landing_keys

        run = _make_run(namespace="myns", layer="bronze", pipeline_name="ingest")
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.SUCCESS
        mock_list_keys.assert_called_once_with(
            s3_config,
            "myns/landing/orders/",
        )
        mock_move_keys.assert_called_once_with(
            s3_config,
            landing_keys,
            "myns/landing/orders/",
            "myns/landing/orders/_processed/r1/",
        )

    @patch(f"{_EXEC_PREFIX}.move_s3_keys")
    @patch(f"{_EXEC_PREFIX}.list_s3_keys")
    @patch(f"{_EXEC_PREFIX}.validate_landing_zones", return_value=[])
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_skips_archive_when_annotation_absent(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_validate_lz: MagicMock,
        mock_list_keys: MagicMock,
        mock_move_keys: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # No archive annotation
        sql = "SELECT * FROM read_csv_auto('{{ landing_zone('orders') }}/*.csv')"

        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run(namespace="myns", layer="bronze", pipeline_name="ingest")
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.SUCCESS
        mock_move_keys.assert_not_called()

    @patch(f"{_EXEC_PREFIX}.move_s3_keys", side_effect=Exception("S3 move failed"))
    @patch(f"{_EXEC_PREFIX}.list_s3_keys", return_value=["myns/landing/z/f.csv"])
    @patch(f"{_EXEC_PREFIX}.validate_landing_zones", return_value=[])
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_archive_failure_warns_but_succeeds(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_validate_lz: MagicMock,
        mock_list_keys: MagicMock,
        mock_move_keys: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        sql = """-- @archive_landing_zones: true
SELECT * FROM read_csv_auto('{{ landing_zone('z') }}/*.csv')"""

        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run(namespace="myns", layer="bronze", pipeline_name="ingest")
        execute_pipeline(run, s3_config, nessie_config)

        # Should still succeed — archive is best-effort
        assert run.status == RunStatus.SUCCESS


class TestExecutePipelineVersionedReads:
    """Tests for published_versions support — reading pinned S3 versions."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text_version")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_executor_reads_published_version(
        self,
        mock_read: MagicMock,
        mock_read_version: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        """When published_versions has a version ID, read_s3_text_version is used."""
        sql_key = "myns/pipelines/silver/orders/pipeline.sql"
        published_versions = {sql_key: "ver-abc-123"}

        mock_read_version.return_value = "SELECT 42 AS value"
        # read_s3_text should not be called for keys with published versions
        mock_read.return_value = None

        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"value": [42]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config, published_versions=published_versions)

        assert run.status == RunStatus.SUCCESS
        mock_read_version.assert_any_call(s3_config, sql_key, "ver-abc-123")

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_executor_falls_back_to_head(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        """Without published_versions, read_s3_text (HEAD) is used."""
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)  # no published_versions

        assert run.status == RunStatus.SUCCESS
        sql_key = "myns/pipelines/silver/orders/pipeline.sql"
        mock_read.assert_any_call(s3_config, sql_key)


class TestNoBranchQualityFailure:
    """Tests for quality gate when branch creation fails (no-branch path)."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=True)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests")
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", side_effect=Exception("Nessie down"))
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_no_branch_quality_failure_marks_failed(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1
        mock_quality.return_value = [
            QualityTestResult(
                test_name="not_null_id",
                test_file="f1",
                severity="error",
                status="fail",
                row_count=3,
                message="3 violation(s) found",
            )
        ]

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "Quality tests failed" in run.error
        assert "not_null_id" in run.error
        mock_merge.assert_not_called()

    @patch(f"{_EXEC_PREFIX}.move_s3_keys")
    @patch(f"{_EXEC_PREFIX}.list_s3_keys")
    @patch(f"{_EXEC_PREFIX}.validate_landing_zones", return_value=[])
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=True)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests")
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", side_effect=Exception("Nessie down"))
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_no_branch_quality_failure_skips_archive(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_validate_lz: MagicMock,
        mock_list_keys: MagicMock,
        mock_move_keys: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        sql = """-- @archive_landing_zones: true
SELECT * FROM read_csv_auto('{{ landing_zone('orders') }}/*.csv')"""

        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1
        mock_quality.return_value = [
            QualityTestResult(
                test_name="t1",
                test_file="f1",
                severity="error",
                status="error",
                row_count=0,
                message="SQL error",
            )
        ]

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        mock_move_keys.assert_not_called()


class TestExecutePipelineStrategyDispatch:
    """Tests for new merge strategy dispatch."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.append_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_append_only_strategy(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_append: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        sql = "-- @merge_strategy: append_only\nSELECT 1 AS id"
        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_append.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_append.assert_called_once()
        assert run.status == RunStatus.SUCCESS

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.delete_insert_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_delete_insert_strategy(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_di: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        sql = "-- @merge_strategy: delete_insert\n-- @unique_key: id\nSELECT 1 AS id"
        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_di.return_value = 5

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_di.assert_called_once()
        assert run.rows_written == 5
        assert run.status == RunStatus.SUCCESS

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.scd2_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_scd2_strategy(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_scd2: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        sql = "-- @merge_strategy: scd2\n-- @unique_key: id\nSELECT 1 AS id"
        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_scd2.return_value = 3

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_scd2.assert_called_once()
        assert run.rows_written == 3

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.snapshot_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_snapshot_strategy(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_snapshot: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        sql = "-- @merge_strategy: snapshot\n-- @partition_column: date\nSELECT 1 AS id"
        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_snapshot.return_value = 10

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_snapshot.assert_called_once()
        assert run.rows_written == 10

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_missing_unique_key_falls_back_to_full_refresh(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # scd2 without unique_key → should fallback
        sql = "-- @merge_strategy: scd2\nSELECT 1 AS id"
        mock_read.side_effect = lambda cfg, key: sql if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        mock_write.assert_called_once()
        assert run.status == RunStatus.SUCCESS


class TestExecutePipelineConfigMerge:
    """Tests for config.yaml + annotation merge logic."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.merge_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_annotation_overrides_config_yaml(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_merge_ice: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # config.yaml says full_refresh, annotation says incremental
        sql = "-- @merge_strategy: incremental\nSELECT 1 AS id"
        config_yaml = "merge_strategy: full_refresh\nunique_key:\n  - id"

        def read_side(cfg, key):
            if key.endswith(".py"):
                return None
            if key.endswith(".sql"):
                return sql
            if key.endswith(".yaml"):
                return config_yaml
            return None

        mock_read.side_effect = read_side
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_merge_ice.return_value = 5

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        # Annotation wins: incremental + unique_key from config.yaml
        mock_merge_ice.assert_called_once()
        assert run.rows_written == 5

    @patch("rat_runner.executor.run_maintenance")
    @patch("rat_runner.executor.merge_branch")
    @patch("rat_runner.executor.delete_branch")
    @patch("rat_runner.executor.create_branch")
    @patch("rat_runner.executor.run_quality_tests", return_value=[])
    @patch("rat_runner.executor.write_iceberg")
    @patch("rat_runner.executor.DuckDBEngine")
    @patch("rat_runner.executor.read_s3_text")
    def test_maintenance_runs_after_success(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write_ice: MagicMock,
        mock_quality: MagicMock,
        mock_create_branch: MagicMock,
        mock_delete_branch: MagicMock,
        mock_merge_branch: MagicMock,
        mock_maintenance: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ) -> None:
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.SUCCESS
        mock_maintenance.assert_called_once()

    @patch("rat_runner.executor.run_maintenance")
    @patch("rat_runner.executor.merge_branch")
    @patch("rat_runner.executor.delete_branch")
    @patch("rat_runner.executor.create_branch")
    @patch("rat_runner.executor.run_quality_tests", return_value=[])
    @patch("rat_runner.executor.write_iceberg")
    @patch("rat_runner.executor.DuckDBEngine")
    @patch("rat_runner.executor.read_s3_text")
    def test_maintenance_skipped_zero_rows(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write_ice: MagicMock,
        mock_quality: MagicMock,
        mock_create_branch: MagicMock,
        mock_delete_branch: MagicMock,
        mock_merge_branch: MagicMock,
        mock_maintenance: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ) -> None:
        mock_read.side_effect = lambda cfg, key: (
            "SELECT 1 AS id WHERE false" if key.endswith(".sql") else None
        )
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": pa.array([], type=pa.int64())})
        mock_engine_cls.return_value = mock_engine

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.SUCCESS
        mock_maintenance.assert_not_called()

    @patch("rat_runner.executor.run_maintenance", side_effect=Exception("maintenance failed"))
    @patch("rat_runner.executor.merge_branch")
    @patch("rat_runner.executor.delete_branch")
    @patch("rat_runner.executor.create_branch")
    @patch("rat_runner.executor.run_quality_tests", return_value=[])
    @patch("rat_runner.executor.write_iceberg")
    @patch("rat_runner.executor.DuckDBEngine")
    @patch("rat_runner.executor.read_s3_text")
    def test_maintenance_failure_does_not_fail_run(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write_ice: MagicMock,
        mock_quality: MagicMock,
        mock_create_branch: MagicMock,
        mock_delete_branch: MagicMock,
        mock_merge_branch: MagicMock,
        mock_maintenance: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ) -> None:
        mock_read.side_effect = lambda cfg, key: "SELECT 1 AS id" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"id": [1]})
        mock_engine_cls.return_value = mock_engine

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        # Maintenance failure should not prevent SUCCESS
        assert run.status == RunStatus.SUCCESS
