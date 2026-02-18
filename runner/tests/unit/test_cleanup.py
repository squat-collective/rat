"""Tests for run cleanup — daemon eviction of expired terminal runs."""

from __future__ import annotations

import time
from unittest.mock import patch

from rat_runner.config import NessieConfig, S3Config
from rat_runner.models import RunState, RunStatus
from rat_runner.server import RunnerServiceImpl


def _make_run(run_id: str, status: RunStatus = RunStatus.PENDING, age: float = 0) -> RunState:
    run = RunState(
        run_id=run_id,
        namespace="ns",
        layer="silver",
        pipeline_name="p",
        trigger="manual",
    )
    run.status = status
    run.created_at = time.time() - age
    return run


class TestRunCleanup:
    def _make_service(self, s3_config: S3Config, nessie_config: NessieConfig) -> RunnerServiceImpl:
        svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=1)
        return svc

    @patch("rat_runner.server.RUN_TTL_SECONDS", 100)
    def test_evicts_terminal_past_ttl(self, s3_config: S3Config, nessie_config: NessieConfig):
        svc = self._make_service(s3_config, nessie_config)
        try:
            # Expired terminal run (200s old, TTL is 100s)
            run = _make_run("r1", RunStatus.SUCCESS, age=200)
            svc._runs["r1"] = run

            svc._evict_expired_runs()

            assert "r1" not in svc._runs
        finally:
            svc.shutdown()

    @patch("rat_runner.server.RUN_TTL_SECONDS", 100)
    def test_preserves_active_runs(self, s3_config: S3Config, nessie_config: NessieConfig):
        svc = self._make_service(s3_config, nessie_config)
        try:
            # Old but still running
            run = _make_run("r1", RunStatus.RUNNING, age=200)
            svc._runs["r1"] = run

            svc._evict_expired_runs()

            assert "r1" in svc._runs
        finally:
            svc.shutdown()

    @patch("rat_runner.server.RUN_TTL_SECONDS", 100)
    def test_preserves_recent_terminal(self, s3_config: S3Config, nessie_config: NessieConfig):
        svc = self._make_service(s3_config, nessie_config)
        try:
            # Terminal but recent (10s old, TTL is 100s)
            run = _make_run("r1", RunStatus.SUCCESS, age=10)
            svc._runs["r1"] = run

            svc._evict_expired_runs()

            assert "r1" in svc._runs
        finally:
            svc.shutdown()

    def test_shutdown_stops_cleanup(self, s3_config: S3Config, nessie_config: NessieConfig):
        svc = self._make_service(s3_config, nessie_config)
        svc.shutdown()

        assert svc._cleanup_stop.is_set()
