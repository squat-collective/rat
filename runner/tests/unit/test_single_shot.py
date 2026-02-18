"""Tests for single-shot pipeline execution mode."""

from __future__ import annotations

import json
from unittest.mock import MagicMock, patch

import pytest

from rat_runner.models import RunStatus

_SINGLE = "rat_runner.single_shot"


class TestRunSingle:
    """Tests for run_single() — single-shot entry point."""

    @patch(f"{_SINGLE}.execute_pipeline")
    @patch(f"{_SINGLE}.NessieConfig.from_env")
    @patch(f"{_SINGLE}.S3Config.from_env")
    def test_success_prints_json_and_exits_0(self, mock_s3, mock_nessie, mock_exec, capsys):
        """Successful run prints JSON with status=success and exits 0."""
        mock_s3.return_value = MagicMock()
        mock_nessie.return_value = MagicMock()

        def fake_execute(run, s3, nessie):
            run.status = RunStatus.SUCCESS
            run.rows_written = 42
            run.duration_ms = 1234

        mock_exec.side_effect = fake_execute

        env = {
            "RUN_MODE": "single",
            "RUN_ID": "run-123",
            "NAMESPACE": "demo",
            "LAYER": "silver",
            "PIPELINE_NAME": "orders",
            "TRIGGER": "manual",
        }
        with patch.dict("os.environ", env, clear=False), pytest.raises(SystemExit) as exc:
            from rat_runner.single_shot import run_single

            run_single()

        assert exc.value.code == 0

        captured = capsys.readouterr()
        result = json.loads(captured.out.strip())
        assert result["status"] == "success"
        assert result["rows_written"] == 42
        assert result["duration_ms"] == 1234
        assert "error" not in result

    @patch(f"{_SINGLE}.execute_pipeline")
    @patch(f"{_SINGLE}.NessieConfig.from_env")
    @patch(f"{_SINGLE}.S3Config.from_env")
    def test_failure_prints_json_and_exits_1(self, mock_s3, mock_nessie, mock_exec, capsys):
        """Failed run prints JSON with error and exits 1."""
        mock_s3.return_value = MagicMock()
        mock_nessie.return_value = MagicMock()

        def fake_execute(run, s3, nessie):
            run.status = RunStatus.FAILED
            run.rows_written = 0
            run.duration_ms = 500
            run.error = "DuckDB OOM"

        mock_exec.side_effect = fake_execute

        env = {
            "RUN_ID": "run-456",
            "NAMESPACE": "demo",
            "LAYER": "bronze",
            "PIPELINE_NAME": "ingest",
            "TRIGGER": "schedule:hourly",
        }
        with patch.dict("os.environ", env, clear=False), pytest.raises(SystemExit) as exc:
            from rat_runner.single_shot import run_single

            run_single()

        assert exc.value.code == 1

        captured = capsys.readouterr()
        result = json.loads(captured.out.strip())
        assert result["status"] == "failed"
        assert result["error"] == "DuckDB OOM"

    def test_missing_env_vars_exits_1(self, capsys):
        """Missing required env vars prints error JSON and exits 1."""
        env = {"RUN_ID": "run-789"}  # missing NAMESPACE, LAYER, PIPELINE_NAME
        with patch.dict("os.environ", env, clear=False):
            # Clear the env vars that might exist from other tests
            for key in ("NAMESPACE", "LAYER", "PIPELINE_NAME"):
                import os

                os.environ.pop(key, None)

            with pytest.raises(SystemExit) as exc:
                from rat_runner.single_shot import run_single

                run_single()

        assert exc.value.code == 1

        captured = capsys.readouterr()
        result = json.loads(captured.out.strip())
        assert result["status"] == "failed"
        assert "Missing required env vars" in result["error"]

    @patch(f"{_SINGLE}.execute_pipeline")
    @patch(f"{_SINGLE}.NessieConfig.from_env")
    @patch(f"{_SINGLE}.S3Config.from_env")
    def test_passes_correct_run_state(self, mock_s3, mock_nessie, mock_exec):
        """Verify RunState is constructed correctly from env vars."""
        mock_s3.return_value = MagicMock()
        mock_nessie.return_value = MagicMock()

        captured_run = None

        def fake_execute(run, s3, nessie):
            nonlocal captured_run
            captured_run = run
            run.status = RunStatus.SUCCESS

        mock_exec.side_effect = fake_execute

        env = {
            "RUN_ID": "abc-def",
            "NAMESPACE": "prod",
            "LAYER": "gold",
            "PIPELINE_NAME": "revenue",
            "TRIGGER": "sensor:upstream",
        }
        with patch.dict("os.environ", env, clear=False), pytest.raises(SystemExit):
            from rat_runner.single_shot import run_single

            run_single()

        assert captured_run is not None
        assert captured_run.run_id == "abc-def"
        assert captured_run.namespace == "prod"
        assert captured_run.layer == "gold"
        assert captured_run.pipeline_name == "revenue"
        assert captured_run.trigger == "sensor:upstream"

    @patch(f"{_SINGLE}.execute_pipeline")
    @patch(f"{_SINGLE}.NessieConfig.from_env")
    @patch(f"{_SINGLE}.S3Config.from_env")
    def test_default_trigger_is_manual(self, mock_s3, mock_nessie, mock_exec):
        """When TRIGGER env var is not set, defaults to 'manual'."""
        mock_s3.return_value = MagicMock()
        mock_nessie.return_value = MagicMock()

        captured_run = None

        def fake_execute(run, s3, nessie):
            nonlocal captured_run
            captured_run = run
            run.status = RunStatus.SUCCESS

        mock_exec.side_effect = fake_execute

        env = {
            "RUN_ID": "run-trig",
            "NAMESPACE": "ns",
            "LAYER": "bronze",
            "PIPELINE_NAME": "raw",
        }
        # Remove TRIGGER if it exists
        import os

        os.environ.pop("TRIGGER", None)

        with patch.dict("os.environ", env, clear=False), pytest.raises(SystemExit):
            from rat_runner.single_shot import run_single

            run_single()

        assert captured_run is not None
        assert captured_run.trigger == "manual"
