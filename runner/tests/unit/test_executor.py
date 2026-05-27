"""Tests for executor — pipeline execution core loop with all 6 features."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pyarrow as pa
import pytest

from rat_runner.config import NessieConfig, S3Config
from rat_runner.executor import execute_pipeline
from rat_runner.models import PipelineConfig, QualityTestResult, RunState, RunStatus


@pytest.fixture(autouse=True)
def _empty_registry():
    """Prevent plugin discovery — tests exercise the built-in fallback dispatch."""
    mock_registry = MagicMock()
    mock_registry.get_strategy.return_value = None
    mock_registry.get_helpers.return_value = {}
    mock_registry.dispatch_hooks.return_value = None

    with patch("rat_runner.executor.PluginRegistry", return_value=mock_registry):
        yield


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
    def test_branch_creation_failure_fails_the_run(
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
        # Behaviour change (was: falls back to main + SUCCESS): Nessie branch
        # creation failure is now FATAL. Falling back to main caused concurrent
        # runs to race on main and produced duplicate rows with no rollback path,
        # so the only safe terminal state for Phase 0 is "branch ready" or "run
        # failed". See _phase0_create_branch.
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "branch creation failed" in run.error
        assert "Nessie down" in run.error
        # Run aborted before any writes — nothing touched main.
        mock_write.assert_not_called()
        mock_merge.assert_not_called()


class TestBranchCreationRetry:
    """Cover the retry/classification behaviour for Phase 0 branch creation.

    These exercise the boundary between the Nessie client (which owns the
    actual retry+backoff loop) and the executor (which translates the
    final outcome into a run state). They drive the REAL retry decorator
    via urllib mocks so the contract — 3 retries on transient, none on
    permanent — is verified end-to-end inside the executor.
    """

    @staticmethod
    def _http_error(code: int, msg: str = "err"):
        import urllib.error

        return urllib.error.HTTPError(
            url="http://nessie/api/v2/trees",
            code=code,
            msg=msg,
            hdrs=None,  # type: ignore[arg-type]
            fp=None,
        )

    @staticmethod
    def _url_error(reason: str = "Connection refused"):
        import urllib.error

        return urllib.error.URLError(reason=reason)

    @patch("rat_runner.nessie.time.sleep")  # don't actually sleep in tests
    @patch("rat_runner.nessie.urllib.request.urlopen")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_transient_error_three_times_exhausts_retries_and_fails_run(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_urlopen: MagicMock,
        mock_sleep: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # Persistent network failure on EVERY urlopen call → the outer
        # create_branch wrapper retries 3 times (4 attempts) then
        # RuntimeError surfaces with our "branch creation failed after
        # 4 attempts" message.
        #
        # Note: create_branch first calls _get_reference, which is itself
        # decorated with @retry_on_transient — so the *raw* sleep count
        # is higher than 3 (nested retries). The contract we care about
        # is at the outer boundary: the run fails fatally, the message
        # mentions our 4-attempt budget, and no data is written.
        mock_urlopen.side_effect = self._url_error("Connection refused")
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine_cls.return_value = MagicMock()

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "branch creation failed after 4 attempts" in run.error
        # At least one retry-backoff sleep occurred (otherwise nothing was
        # retried). Exact count depends on nested retry stacking; the
        # contract is "we DID retry, then gave up", not the precise N.
        assert mock_sleep.call_count >= 3
        # Run aborted before any writes — nothing touched main.
        mock_write.assert_not_called()

    @patch("rat_runner.nessie.time.sleep")
    @patch("rat_runner.nessie.urllib.request.urlopen")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_permanent_error_fails_immediately_without_retry(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_urlopen: MagicMock,
        mock_sleep: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # 400 Bad Request is a permanent 4xx → no retry, fail run on the
        # first attempt.
        mock_urlopen.side_effect = self._http_error(400, "Bad Request")
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine_cls.return_value = MagicMock()

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "branch creation failed" in run.error
        mock_sleep.assert_not_called()  # zero retries
        # Only the single failing call was made — no retry.
        assert mock_urlopen.call_count == 1
        mock_write.assert_not_called()

    @patch("rat_runner.nessie.time.sleep")
    @patch("rat_runner.nessie.urllib.request.urlopen")
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_succeeds_on_retry_attempt_two(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_urlopen: MagicMock,
        mock_sleep: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        # 1st attempt: transient 503 — sleeps once, retries
        # 2nd attempt: succeeds (get reference + POST)
        import json
        import urllib.error

        # Nessie 0.99.x wraps reference responses under a top-level
        # "reference" key for both GET /trees/{ref} and POST /trees.
        ok_ref = MagicMock()
        ok_ref.read.return_value = json.dumps(
            {"reference": {"name": "main", "hash": "abc123", "type": "BRANCH"}}
        ).encode()
        ok_ref.__enter__ = lambda s: s
        ok_ref.__exit__ = MagicMock(return_value=False)

        ok_create = MagicMock()
        ok_create.read.return_value = json.dumps(
            {"reference": {"name": "run-r1", "hash": "def456", "type": "BRANCH"}}
        ).encode()
        ok_create.__enter__ = lambda s: s
        ok_create.__exit__ = MagicMock(return_value=False)

        # Sequence of urlopen calls during create_branch:
        # attempt 1: _get_reference → 503 (decorator restarts the whole call)
        # attempt 2: _get_reference (ok_ref) → POST (ok_create)
        mock_urlopen.side_effect = [
            urllib.error.HTTPError(
                url="http://nessie/api/v2/trees/main",
                code=503,
                msg="Service Unavailable",
                hdrs=None,  # type: ignore[arg-type]
                fp=None,
            ),
            ok_ref,
            ok_create,
        ]

        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        # Branch was eventually created → run reaches SUCCESS via normal path.
        assert run.status == RunStatus.SUCCESS
        assert run.branch == f"run-{run.run_id}"
        # Exactly one backoff sleep (0.5s) before the retry succeeded.
        mock_sleep.assert_called_once_with(0.5)


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


class TestBranchCreationFailureAborts:
    """When Nessie branch creation fails, the run aborts BEFORE any side effects.

    This class replaces the old TestNoBranchQualityFailure — the "no branch"
    code path was removed entirely (branch creation is now fatal), so the
    quality-test and archive logic must never run when Phase 0 fails.
    """

    @patch(f"{_EXEC_PREFIX}.has_error_failures")
    @patch(f"{_EXEC_PREFIX}.run_quality_tests")
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", side_effect=Exception("Nessie down"))
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_branch_creation_failure_skips_quality_and_write(
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

        assert run.status == RunStatus.FAILED
        assert "branch creation failed" in run.error
        # Nothing downstream of Phase 0 should have run.
        mock_write.assert_not_called()
        mock_quality.assert_not_called()
        mock_has_fail.assert_not_called()
        mock_merge.assert_not_called()

    @patch(f"{_EXEC_PREFIX}.move_s3_keys")
    @patch(f"{_EXEC_PREFIX}.list_s3_keys")
    @patch(f"{_EXEC_PREFIX}.validate_landing_zones", return_value=[])
    @patch(f"{_EXEC_PREFIX}.has_error_failures")
    @patch(f"{_EXEC_PREFIX}.run_quality_tests")
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", side_effect=Exception("Nessie down"))
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_branch_creation_failure_skips_archive(
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


class TestExecutePipelinePluginType:
    """Tests for plugin-provided pipeline types (rat.pipeline_types)."""

    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_plugin_pipeline_type_detected_and_executed(
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
    ) -> None:
        """A pipeline.<ext> file is detected and dispatched to the plugin type."""
        # Only a pipeline.prql file exists — no pipeline.py / pipeline.sql.
        mock_read.side_effect = lambda cfg, key: (
            "from data" if key.endswith("pipeline.prql") else None
        )
        mock_write.return_value = 1

        # A fake plugin pipeline type that owns the .prql extension.
        fake_type = MagicMock()
        fake_type.name = "prql"
        fake_type.file_extension = "prql"
        fake_type.execute.return_value = pa.table({"value": [7]})

        registry = MagicMock()
        registry.get_strategy.return_value = None
        registry.get_helpers.return_value = {}
        registry.dispatch_hooks.return_value = None
        registry.pipeline_type_names.return_value = ["prql"]
        registry.get_pipeline_type.return_value = fake_type

        run = _make_run()
        with patch(f"{_EXEC_PREFIX}.PluginRegistry", return_value=registry):
            execute_pipeline(run, s3_config, nessie_config)

        # The executor looked for the plugin type's file...
        mock_read.assert_any_call(s3_config, "myns/pipelines/silver/orders/pipeline.prql")
        # ...and dispatched execution to the plugin, passing the file's contents.
        fake_type.execute.assert_called_once()
        assert fake_type.execute.call_args[0][0] == "from data"
        assert run.status == RunStatus.SUCCESS


class TestPhase5MergeFailure:
    """Phase 5 merge failures must:
      * NOT delete the ephemeral branch (it holds the only copy of the data)
      * POST a failed_merges audit row to ratd
      * Mark the run FAILED with a recovery-friendly error message
    """

    @staticmethod
    def _http_error(code: int, msg: str = "err"):
        import urllib.error

        return urllib.error.HTTPError(
            url="http://nessie/api/v2/trees/main/history/merge",
            code=code,
            msg=msg,
            hdrs=None,  # type: ignore[arg-type]
            fp=None,
        )

    @patch(f"{_EXEC_PREFIX}.record_failed_merge")
    @patch(f"{_EXEC_PREFIX}._get_reference")
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_merge_permanent_4xx_fails_terminally_branch_retained(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_get_ref: MagicMock,
        mock_audit: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        """400 on the merge POST → branch NOT deleted; audit row sent."""
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1
        mock_get_ref.return_value = {"hash": "deadbeef"}
        mock_merge.side_effect = self._http_error(400, "Bad Request")

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "branch merge failed" in run.error
        assert "retained for recovery" in run.error
        # Branch retained — delete_branch was NOT called in the finally.
        mock_delete.assert_not_called()
        # Audit POST sent with classified error_kind.
        mock_audit.assert_called_once()
        kwargs = mock_audit.call_args.kwargs
        assert kwargs["error_kind"] == "permanent_4xx"

    @patch(f"{_EXEC_PREFIX}.record_failed_merge")
    @patch(f"{_EXEC_PREFIX}._get_reference")
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_merge_retries_exhausted_branch_retained(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_get_ref: MagicMock,
        mock_audit: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        """503 on merge after retries exhausted → branch retained, audit sent.

        The executor sees the URLError that bubbles out of merge_branch after
        the inner @retry_on_transient decorator gives up; classify as
        transient_exhausted.
        """
        import urllib.error

        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1
        mock_get_ref.return_value = {"hash": "deadbeef"}
        mock_merge.side_effect = urllib.error.URLError("Connection refused")

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        assert "retained for recovery" in run.error
        mock_delete.assert_not_called()  # branch NOT swept
        mock_audit.assert_called_once()
        assert mock_audit.call_args.kwargs["error_kind"] == "transient_exhausted"

    @patch(f"{_EXEC_PREFIX}.record_failed_merge")
    @patch(f"{_EXEC_PREFIX}._get_reference")
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.merge_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_merge_409_conflict_exhausted_branch_retained(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_merge: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_get_ref: MagicMock,
        mock_audit: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        """409 bubbling out of merge_branch (after inner refetches gave up)
        → audit row with error_kind='conflict_exhausted'."""
        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1
        mock_get_ref.return_value = {"hash": "deadbeef"}
        mock_merge.side_effect = self._http_error(409, "Conflict")

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        assert run.status == RunStatus.FAILED
        mock_delete.assert_not_called()
        mock_audit.assert_called_once()
        assert mock_audit.call_args.kwargs["error_kind"] == "conflict_exhausted"
        assert "retained for recovery" in run.error


class TestPhase5MergeTransientThenSuccess:
    """End-to-end transient-then-success path: a 503 on a sub-call inside
    merge_branch is retried by the @retry_on_transient decorator and the
    run reaches SUCCESS without an audit row."""

    @patch(f"{_EXEC_PREFIX}.record_failed_merge")
    @patch("rat_runner.nessie.time.sleep")
    @patch("rat_runner.nessie.urllib.request.urlopen")
    @patch(f"{_EXEC_PREFIX}.has_error_failures", return_value=False)
    @patch(f"{_EXEC_PREFIX}.run_quality_tests", return_value=[])
    @patch(f"{_EXEC_PREFIX}.delete_branch")
    @patch(f"{_EXEC_PREFIX}.create_branch", return_value="hash123")
    @patch(f"{_EXEC_PREFIX}.write_iceberg")
    @patch(f"{_EXEC_PREFIX}.DuckDBEngine")
    @patch(f"{_EXEC_PREFIX}.read_s3_text")
    def test_merge_transient_error_retries_then_succeeds(
        self,
        mock_read: MagicMock,
        mock_engine_cls: MagicMock,
        mock_write: MagicMock,
        mock_create: MagicMock,
        mock_delete: MagicMock,
        mock_quality: MagicMock,
        mock_has_fail: MagicMock,
        mock_urlopen: MagicMock,
        mock_sleep: MagicMock,
        mock_audit: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ):
        """The merge phase calls the REAL nessie code path. urlopen yields
        a 503 once on _get_reference, retries, then succeeds. No audit."""
        import json
        import urllib.error

        def _ok(data: dict) -> MagicMock:
            r = MagicMock()
            r.read.return_value = json.dumps(data).encode()
            r.__enter__ = lambda s: s
            r.__exit__ = MagicMock(return_value=False)
            return r

        ref_payload = {"reference": {"name": "main", "hash": "abc", "type": "BRANCH"}}
        # Two `_get_reference` calls before the merge POST in Phase 5 (executor
        # captures hashes pre-merge), then merge_branch itself does src+tgt+POST.
        # On 503 _get_reference retries internally.
        merge_ok = MagicMock()
        merge_ok.read.return_value = b"{}"
        merge_ok.__enter__ = lambda s: s
        merge_ok.__exit__ = MagicMock(return_value=False)

        # The executor's pre-merge hash capture is the FIRST consumer of urlopen
        # (2× _get_reference). Then merge_branch consumes the rest.
        responses: list = [
            _ok(ref_payload),  # pre-merge src hash
            _ok(ref_payload),  # pre-merge tgt hash
            urllib.error.HTTPError(  # merge_branch: src _get_reference fails 503
                url="x", code=503, msg="Service Unavailable",
                hdrs=None, fp=None,  # type: ignore[arg-type]
            ),
            _ok(ref_payload),  # retry succeeds
            _ok(ref_payload),  # merge_branch: tgt _get_reference
            merge_ok,          # merge POST 200
        ]
        mock_urlopen.side_effect = responses

        mock_read.side_effect = lambda cfg, key: "SELECT 1" if key.endswith(".sql") else None
        mock_engine = MagicMock()
        mock_engine.query_arrow.return_value = pa.table({"x": [1]})
        mock_engine_cls.return_value = mock_engine
        mock_write.return_value = 1

        run = _make_run()
        execute_pipeline(run, s3_config, nessie_config)

        # SUCCESS — transient handled by the decorator, no audit row.
        assert run.status == RunStatus.SUCCESS, run.error
        mock_audit.assert_not_called()
        # Backoff sleep DID fire (we wouldn't have retried without it).
        assert mock_sleep.call_count >= 1
