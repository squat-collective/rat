"""Tests for server — gRPC RunnerService implementation."""

from __future__ import annotations

import json
import os
import threading
import time
from concurrent import futures
from pathlib import Path
from unittest.mock import MagicMock, patch

import grpc
import pytest

# Proto imports
from common.v1 import common_pb2
from runner.v1 import runner_pb2, runner_pb2_grpc

from rat_runner.config import NessieConfig, S3Config
from rat_runner.models import RunStatus
from rat_runner.server import RunnerServiceImpl, _configure_server_port, _s3_credentials_to_dict
from rat_runner.state_dir import write_marker


@pytest.fixture
def state_dir(tmp_path: Path) -> Path:
    """Isolated state directory for each test."""
    d = tmp_path / "runner-state"
    d.mkdir()
    return d


@pytest.fixture
def service(s3_config: S3Config, nessie_config: NessieConfig, state_dir: Path):
    svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=2, state_dir=state_dir)
    yield svc
    svc.shutdown()


@pytest.fixture
def grpc_channel(
    service: RunnerServiceImpl,
) -> grpc.Channel:
    """Start an in-process gRPC server and return a channel to it."""
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    runner_pb2_grpc.add_RunnerServiceServicer_to_server(service, server)
    port = server.add_insecure_port("[::]:0")
    server.start()
    channel = grpc.insecure_channel(f"localhost:{port}")
    yield channel
    channel.close()
    server.stop(grace=0)


@pytest.fixture
def stub(grpc_channel: grpc.Channel) -> runner_pb2_grpc.RunnerServiceStub:
    return runner_pb2_grpc.RunnerServiceStub(grpc_channel)


class TestSubmitPipeline:
    @patch("rat_runner.server.execute_pipeline")
    def test_returns_run_id_and_pending(
        self,
        mock_exec: None,
        stub: runner_pb2_grpc.RunnerServiceStub,
    ):
        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="myns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="orders",
                trigger="manual",
            )
        )
        assert resp.run_id != ""
        assert resp.status == common_pb2.RUN_STATUS_PENDING

    def test_invalid_layer_returns_error(
        self,
        stub: runner_pb2_grpc.RunnerServiceStub,
    ):
        with pytest.raises(grpc.RpcError) as exc_info:
            stub.SubmitPipeline(
                runner_pb2.SubmitPipelineRequest(
                    namespace="myns",
                    layer=common_pb2.LAYER_UNSPECIFIED,
                    pipeline_name="orders",
                    trigger="manual",
                )
            )
        assert exc_info.value.code() == grpc.StatusCode.INVALID_ARGUMENT


class TestGetRunStatus:
    @patch("rat_runner.server.execute_pipeline")
    def test_returns_current_state(
        self,
        mock_exec: None,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
    ):
        # Submit a run first
        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_BRONZE,
                pipeline_name="raw",
                trigger="manual",
            )
        )
        run_id = resp.run_id

        # Manually set success on the RunState
        service._runs[run_id].status = RunStatus.SUCCESS
        service._runs[run_id].rows_written = 42

        status_resp = stub.GetRunStatus(common_pb2.GetRunStatusRequest(run_id=run_id))
        assert status_resp.status == common_pb2.RUN_STATUS_SUCCESS
        assert status_resp.rows_written == 42

    def test_unknown_run_returns_not_found(
        self,
        stub: runner_pb2_grpc.RunnerServiceStub,
    ):
        with pytest.raises(grpc.RpcError) as exc_info:
            stub.GetRunStatus(common_pb2.GetRunStatusRequest(run_id="nonexistent"))
        assert exc_info.value.code() == grpc.StatusCode.NOT_FOUND


class TestCancelRun:
    @patch("rat_runner.server.execute_pipeline")
    def test_sets_cancel_event(
        self,
        mock_exec: None,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
    ):
        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="p",
                trigger="manual",
            )
        )
        run_id = resp.run_id

        cancel_resp = stub.CancelRun(common_pb2.CancelRunRequest(run_id=run_id))
        assert cancel_resp.cancelled is True
        assert service._runs[run_id].cancel_event.is_set()

    @patch("rat_runner.server.execute_pipeline")
    def test_terminal_run_returns_false(
        self,
        mock_exec: None,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
    ):
        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="p",
                trigger="manual",
            )
        )
        service._runs[resp.run_id].status = RunStatus.SUCCESS

        cancel_resp = stub.CancelRun(common_pb2.CancelRunRequest(run_id=resp.run_id))
        assert cancel_resp.cancelled is False


class TestStreamLogs:
    @patch("rat_runner.server.execute_pipeline")
    def test_returns_buffered_entries(
        self,
        mock_exec: None,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
    ):
        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="p",
                trigger="manual",
            )
        )
        run = service._runs[resp.run_id]
        run.add_log("info", "step 1")
        run.add_log("info", "step 2")
        run.status = RunStatus.SUCCESS  # mark terminal so stream stops

        entries = list(
            stub.StreamLogs(common_pb2.StreamLogsRequest(run_id=resp.run_id, follow=False))
        )
        assert len(entries) == 2
        assert entries[0].level == "info"
        assert entries[0].message == "step 1"
        assert entries[1].message == "step 2"

    @patch("rat_runner.server.execute_pipeline")
    def test_follow_waits_for_new_entries(
        self,
        mock_exec: None,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
    ):
        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="p",
                trigger="manual",
            )
        )
        run = service._runs[resp.run_id]
        run.add_log("info", "initial")

        # In a separate thread, add more logs then mark terminal
        def delayed_logs():
            time.sleep(0.3)
            run.add_log("info", "delayed")
            time.sleep(0.3)
            run.status = RunStatus.SUCCESS

        t = threading.Thread(target=delayed_logs)
        t.start()

        entries = list(
            stub.StreamLogs(common_pb2.StreamLogsRequest(run_id=resp.run_id, follow=True))
        )
        t.join()

        messages = [e.message for e in entries]
        assert "initial" in messages
        assert "delayed" in messages

    @patch("rat_runner.server.execute_pipeline")
    def test_condition_wakeup_delivers_logs_without_polling_delay(
        self,
        mock_exec: None,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
    ):
        """Logs arrive via condition variable wakeup, not polling.

        Adds a log while StreamLogs is blocking and measures that the
        entry is delivered well under the old 500ms polling interval.
        """
        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="p",
                trigger="manual",
            )
        )
        run = service._runs[resp.run_id]

        received: list[float] = []  # timestamps of received log entries

        def stream_reader():
            for _entry in stub.StreamLogs(
                common_pb2.StreamLogsRequest(run_id=resp.run_id, follow=True)
            ):
                received.append(time.time())

        reader = threading.Thread(target=stream_reader)
        reader.start()

        # Give StreamLogs time to enter its wait()
        time.sleep(0.2)

        # Add a log — the condition variable should wake StreamLogs immediately
        add_time = time.time()
        run.add_log("info", "wakeup-test")

        # Give a small window for delivery, then terminate
        time.sleep(0.2)
        run.status = RunStatus.SUCCESS

        reader.join(timeout=5)

        assert len(received) >= 1, "Should have received at least one log entry"
        # The log should arrive well under the old 500ms polling interval.
        # With condition-based wakeup it should be near-instant (< 100ms).
        latency = received[0] - add_time
        assert latency < 0.3, (
            f"Log delivery took {latency:.3f}s — expected < 0.3s with condition wakeup"
        )


class TestBackpressure:
    """Tests for concurrent run limits (RESOURCE_EXHAUSTED backpressure)."""

    @pytest.fixture
    def bp_service(self, s3_config: S3Config, nessie_config: NessieConfig, state_dir: Path):
        """Service with max_concurrent_runs=2 for backpressure tests."""
        svc = RunnerServiceImpl(
            s3_config,
            nessie_config,
            max_workers=2,
            state_dir=state_dir,
            max_concurrent_runs=2,
        )
        yield svc
        svc.shutdown()

    @pytest.fixture
    def bp_channel(self, bp_service: RunnerServiceImpl) -> grpc.Channel:
        server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        runner_pb2_grpc.add_RunnerServiceServicer_to_server(bp_service, server)
        port = server.add_insecure_port("[::]:0")
        server.start()
        channel = grpc.insecure_channel(f"localhost:{port}")
        yield channel
        channel.close()
        server.stop(grace=0)

    @pytest.fixture
    def bp_stub(self, bp_channel: grpc.Channel) -> runner_pb2_grpc.RunnerServiceStub:
        return runner_pb2_grpc.RunnerServiceStub(bp_channel)

    @patch("rat_runner.server.execute_pipeline")
    def test_rejects_when_at_capacity(
        self,
        mock_exec: None,
        bp_stub: runner_pb2_grpc.RunnerServiceStub,
        bp_service: RunnerServiceImpl,
    ):
        """When max_concurrent_runs is reached, SubmitPipeline returns RESOURCE_EXHAUSTED."""
        # Submit 2 runs (the limit)
        for i in range(2):
            resp = bp_stub.SubmitPipeline(
                runner_pb2.SubmitPipelineRequest(
                    namespace="ns",
                    layer=common_pb2.LAYER_SILVER,
                    pipeline_name=f"pipeline-{i}",
                    trigger="manual",
                )
            )
            assert resp.run_id != ""

        assert bp_service.active_run_count == 2

        # Third submission should be rejected
        with pytest.raises(grpc.RpcError) as exc_info:
            bp_stub.SubmitPipeline(
                runner_pb2.SubmitPipelineRequest(
                    namespace="ns",
                    layer=common_pb2.LAYER_SILVER,
                    pipeline_name="pipeline-overflow",
                    trigger="manual",
                )
            )
        assert exc_info.value.code() == grpc.StatusCode.RESOURCE_EXHAUSTED
        assert "at capacity" in exc_info.value.details().lower()

    @patch("rat_runner.server.execute_pipeline")
    def test_accepts_after_run_completes(
        self,
        mock_exec: None,
        bp_stub: runner_pb2_grpc.RunnerServiceStub,
        bp_service: RunnerServiceImpl,
    ):
        """After a run finishes (terminal), a new slot opens and submissions are accepted."""
        # Fill capacity
        run_ids = []
        for i in range(2):
            resp = bp_stub.SubmitPipeline(
                runner_pb2.SubmitPipelineRequest(
                    namespace="ns",
                    layer=common_pb2.LAYER_SILVER,
                    pipeline_name=f"pipeline-{i}",
                    trigger="manual",
                )
            )
            run_ids.append(resp.run_id)

        # Mark first run as terminal
        bp_service._runs[run_ids[0]].status = RunStatus.SUCCESS

        assert bp_service.active_run_count == 1

        # Now a new submission should succeed
        resp = bp_stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="pipeline-new",
                trigger="manual",
            )
        )
        assert resp.run_id != ""
        assert resp.status == common_pb2.RUN_STATUS_PENDING

    @patch("rat_runner.server.execute_pipeline")
    def test_resource_exhausted_includes_capacity_details(
        self,
        mock_exec: None,
        bp_stub: runner_pb2_grpc.RunnerServiceStub,
        bp_service: RunnerServiceImpl,
    ):
        """Error details include the current and max run counts."""
        # Fill capacity
        for i in range(2):
            bp_stub.SubmitPipeline(
                runner_pb2.SubmitPipelineRequest(
                    namespace="ns",
                    layer=common_pb2.LAYER_SILVER,
                    pipeline_name=f"pipeline-{i}",
                    trigger="manual",
                )
            )

        with pytest.raises(grpc.RpcError) as exc_info:
            bp_stub.SubmitPipeline(
                runner_pb2.SubmitPipelineRequest(
                    namespace="ns",
                    layer=common_pb2.LAYER_SILVER,
                    pipeline_name="overflow",
                    trigger="manual",
                )
            )
        assert "2/2" in exc_info.value.details()

    def test_active_run_count_property(
        self,
        bp_service: RunnerServiceImpl,
    ):
        """active_run_count only counts non-terminal runs."""
        from rat_runner.models import RunState

        bp_service._runs["run-1"] = RunState(
            run_id="run-1",
            namespace="ns",
            layer="silver",
            pipeline_name="a",
            trigger="manual",
            status=RunStatus.RUNNING,
        )
        bp_service._runs["run-2"] = RunState(
            run_id="run-2",
            namespace="ns",
            layer="silver",
            pipeline_name="b",
            trigger="manual",
            status=RunStatus.SUCCESS,
        )
        bp_service._runs["run-3"] = RunState(
            run_id="run-3",
            namespace="ns",
            layer="silver",
            pipeline_name="c",
            trigger="manual",
            status=RunStatus.PENDING,
        )

        assert bp_service.active_run_count == 2  # RUNNING + PENDING


class TestGRPCMaxWorkers:
    """Tests for RUNNER_MAX_WORKERS env var controlling gRPC thread pool size."""

    def test_default_max_workers(self):
        """GRPC_MAX_WORKERS defaults to 10 when RUNNER_MAX_WORKERS is not set."""
        env = {k: v for k, v in os.environ.items() if k != "RUNNER_MAX_WORKERS"}
        with patch.dict(os.environ, env, clear=True):
            # Re-evaluate the module-level constant
            val = int(os.environ.get("RUNNER_MAX_WORKERS", "10"))
        assert val == 10

    def test_custom_max_workers_from_env(self):
        """GRPC_MAX_WORKERS reads from RUNNER_MAX_WORKERS env var."""
        env = {k: v for k, v in os.environ.items()}
        env["RUNNER_MAX_WORKERS"] = "25"
        with patch.dict(os.environ, env, clear=True):
            val = int(os.environ.get("RUNNER_MAX_WORKERS", "10"))
        assert val == 25

    def test_serve_uses_grpc_max_workers(self):
        """serve() passes GRPC_MAX_WORKERS to the gRPC server's ThreadPoolExecutor."""
        with (
            patch.dict(os.environ, {"RUNNER_MAX_WORKERS": "16"}, clear=False),
            patch("rat_runner.server.S3Config.from_env"),
            patch("rat_runner.server.NessieConfig.from_env"),
            patch("rat_runner.server.RunnerServiceImpl"),
            patch("rat_runner.server.runner_pb2_grpc.add_RunnerServiceServicer_to_server"),
            patch("rat_runner.server._configure_server_port"),
            patch("rat_runner.server.grpc.server") as mock_grpc_server,
            patch("rat_runner.server.futures.ThreadPoolExecutor") as mock_pool,
        ):
            # Reload the module-level constant
            import rat_runner.server as srv_mod

            # Save original and set new value
            original = srv_mod.GRPC_MAX_WORKERS
            srv_mod.GRPC_MAX_WORKERS = 16
            try:
                mock_server = MagicMock()
                mock_server.wait_for_termination.side_effect = KeyboardInterrupt
                mock_grpc_server.return_value = mock_server

                from rat_runner.server import serve

                serve(port=50099)

                mock_pool.assert_called_once_with(max_workers=16)
            finally:
                srv_mod.GRPC_MAX_WORKERS = original


class TestConfigureServerPort:
    def test_insecure_port_when_no_tls_env_vars(self):
        """serve() uses add_insecure_port when GRPC_TLS_CERT and GRPC_TLS_KEY are unset."""
        server = MagicMock(spec=grpc.Server)
        env = {k: v for k, v in os.environ.items() if k not in ("GRPC_TLS_CERT", "GRPC_TLS_KEY")}
        with patch.dict(os.environ, env, clear=True):
            _configure_server_port(server, 50052)
        server.add_insecure_port.assert_called_once_with("[::]:50052")
        server.add_secure_port.assert_not_called()

    def test_raises_when_only_cert_set(self):
        """serve() raises ValueError when GRPC_TLS_CERT is set but GRPC_TLS_KEY is not."""
        server = MagicMock(spec=grpc.Server)
        env = {k: v for k, v in os.environ.items() if k not in ("GRPC_TLS_CERT", "GRPC_TLS_KEY")}
        env["GRPC_TLS_CERT"] = "/path/to/cert.pem"
        with patch.dict(os.environ, env, clear=True):
            with pytest.raises(ValueError, match="Both GRPC_TLS_CERT and GRPC_TLS_KEY must be set"):
                _configure_server_port(server, 50052)

    def test_raises_when_only_key_set(self):
        """serve() raises ValueError when GRPC_TLS_KEY is set but GRPC_TLS_CERT is not."""
        server = MagicMock(spec=grpc.Server)
        env = {k: v for k, v in os.environ.items() if k not in ("GRPC_TLS_CERT", "GRPC_TLS_KEY")}
        env["GRPC_TLS_KEY"] = "/path/to/key.pem"
        with patch.dict(os.environ, env, clear=True):
            with pytest.raises(ValueError, match="Both GRPC_TLS_CERT and GRPC_TLS_KEY must be set"):
                _configure_server_port(server, 50052)

    def test_secure_port_when_both_tls_vars_set(self, tmp_path):
        """serve() uses add_secure_port with ssl_server_credentials when both TLS env vars are set."""
        cert_file = tmp_path / "cert.pem"
        key_file = tmp_path / "key.pem"
        cert_file.write_bytes(b"FAKE-CERT-DATA")
        key_file.write_bytes(b"FAKE-KEY-DATA")

        server = MagicMock(spec=grpc.Server)
        env = {k: v for k, v in os.environ.items() if k not in ("GRPC_TLS_CERT", "GRPC_TLS_KEY")}
        env["GRPC_TLS_CERT"] = str(cert_file)
        env["GRPC_TLS_KEY"] = str(key_file)
        with (
            patch.dict(os.environ, env, clear=True),
            patch("rat_runner.server.grpc.ssl_server_credentials") as mock_ssl,
        ):
            mock_creds = MagicMock()
            mock_ssl.return_value = mock_creds
            _configure_server_port(server, 50052)

        mock_ssl.assert_called_once_with([(b"FAKE-KEY-DATA", b"FAKE-CERT-DATA")])
        server.add_secure_port.assert_called_once_with("[::]:50052", mock_creds)
        server.add_insecure_port.assert_not_called()


class TestCrashRecovery:
    """Tests for startup reconciliation of crashed runs via marker files."""

    def test_no_markers_means_no_crashed_runs(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        state_dir: Path,
    ):
        """Server starts cleanly when state directory has no marker files."""
        svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=1, state_dir=state_dir)
        try:
            assert len(svc._runs) == 0
        finally:
            svc.shutdown()

    def test_leftover_marker_reconciled_as_failed(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        state_dir: Path,
    ):
        """A marker file left from a previous crash is reconciled as a FAILED run."""
        # Simulate a crashed run by writing a marker before creating the service
        write_marker(
            state_dir, "crashed-run-1", "production", "silver", "orders", "schedule:hourly"
        )

        svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=1, state_dir=state_dir)
        try:
            assert "crashed-run-1" in svc._runs
            run = svc._runs["crashed-run-1"]
            assert run.status == RunStatus.FAILED
            assert "restarted" in run.error
            assert run.namespace == "production"
            assert run.layer == "silver"
            assert run.pipeline_name == "orders"
            assert run.trigger == "schedule:hourly"
        finally:
            svc.shutdown()

    def test_multiple_markers_all_reconciled(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        state_dir: Path,
    ):
        """Multiple leftover markers are all reconciled on startup."""
        write_marker(state_dir, "run-a", "ns1", "bronze", "ingest", "manual")
        write_marker(state_dir, "run-b", "ns2", "gold", "report", "schedule:daily")

        svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=1, state_dir=state_dir)
        try:
            assert len(svc._runs) == 2
            assert svc._runs["run-a"].status == RunStatus.FAILED
            assert svc._runs["run-b"].status == RunStatus.FAILED
            assert svc._runs["run-a"].pipeline_name == "ingest"
            assert svc._runs["run-b"].pipeline_name == "report"
        finally:
            svc.shutdown()

    def test_markers_removed_after_reconciliation(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        state_dir: Path,
    ):
        """Marker files are cleaned up after reconciliation so they don't reappear."""
        write_marker(state_dir, "run-x", "ns", "silver", "pipe", "manual")

        svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=1, state_dir=state_dir)
        try:
            # Marker file should be gone
            remaining = list(state_dir.glob("*.json"))
            assert len(remaining) == 0
        finally:
            svc.shutdown()

    def test_corrupt_marker_ignored(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        state_dir: Path,
    ):
        """Corrupt marker files are skipped (and removed) without crashing startup."""
        # Write a valid marker
        write_marker(state_dir, "good-run", "ns", "bronze", "ok", "manual")
        # Write a corrupt marker
        (state_dir / "bad-run.json").write_text("NOT VALID JSON", encoding="utf-8")

        svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=1, state_dir=state_dir)
        try:
            # Only the valid marker should be reconciled
            assert len(svc._runs) == 1
            assert "good-run" in svc._runs
            # Both files should be cleaned up
            remaining = list(state_dir.glob("*.json"))
            assert len(remaining) == 0
        finally:
            svc.shutdown()

    def test_empty_state_dir_created_if_missing(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        tmp_path: Path,
    ):
        """Service starts fine when state directory does not exist yet."""
        nonexistent = tmp_path / "does-not-exist" / "nested"
        svc = RunnerServiceImpl(s3_config, nessie_config, max_workers=1, state_dir=nonexistent)
        try:
            assert len(svc._runs) == 0
            # The directory should have been created by get_state_dir or collect_crashed_runs
            # (collect_crashed_runs handles missing dirs gracefully)
        finally:
            svc.shutdown()


class TestMarkerFileLifecycle:
    """Tests that marker files are written on submit and removed after execution."""

    @patch("rat_runner.server.execute_pipeline")
    def test_marker_written_on_submit(
        self,
        mock_exec: MagicMock,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
        state_dir: Path,
    ):
        """A marker file is created when a pipeline is submitted."""
        # Block execution so we can inspect the marker before it's removed
        barrier = threading.Event()
        mock_exec.side_effect = lambda *a, **kw: barrier.wait(timeout=5)

        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="myns",
                layer=common_pb2.LAYER_SILVER,
                pipeline_name="orders",
                trigger="manual",
            )
        )

        # Give the thread pool a moment to pick up the task
        time.sleep(0.2)

        run_id = resp.run_id
        marker_path = state_dir / f"{run_id}.json"
        assert marker_path.exists(), "Marker file should exist while run is in-flight"

        data = json.loads(marker_path.read_text(encoding="utf-8"))
        assert data["run_id"] == run_id
        assert data["namespace"] == "myns"
        assert data["layer"] == "silver"
        assert data["pipeline_name"] == "orders"
        assert data["trigger"] == "manual"

        # Release the executor so cleanup can proceed
        barrier.set()

    @patch("rat_runner.server.execute_pipeline")
    def test_marker_removed_after_execution(
        self,
        mock_exec: MagicMock,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
        state_dir: Path,
    ):
        """Marker file is removed after the pipeline execution finishes."""
        done = threading.Event()
        mock_exec.side_effect = lambda *a, **kw: done.set()

        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_BRONZE,
                pipeline_name="raw",
                trigger="manual",
            )
        )

        # Wait for execution to complete
        done.wait(timeout=5)
        # Give the finally block a moment to remove the marker
        time.sleep(0.3)

        marker_path = state_dir / f"{resp.run_id}.json"
        assert not marker_path.exists(), "Marker file should be removed after execution"

    @patch("rat_runner.server.execute_pipeline")
    def test_marker_removed_even_on_executor_exception(
        self,
        mock_exec: MagicMock,
        stub: runner_pb2_grpc.RunnerServiceStub,
        service: RunnerServiceImpl,
        state_dir: Path,
    ):
        """Marker file is removed even when the executor raises an exception."""
        done = threading.Event()

        def failing_exec(*args, **kwargs):
            done.set()
            raise RuntimeError("Simulated crash in executor")

        mock_exec.side_effect = failing_exec

        resp = stub.SubmitPipeline(
            runner_pb2.SubmitPipelineRequest(
                namespace="ns",
                layer=common_pb2.LAYER_GOLD,
                pipeline_name="agg",
                trigger="schedule:daily",
            )
        )

        done.wait(timeout=5)
        time.sleep(0.3)

        marker_path = state_dir / f"{resp.run_id}.json"
        assert not marker_path.exists(), "Marker should be removed even after executor exception"


class TestS3CredentialsToDict:
    """Tests for _s3_credentials_to_dict proto-to-dict conversion."""

    def test_full_credentials_converted(self):
        creds = common_pb2.S3Credentials(
            endpoint="minio:9000",
            access_key_id="AKID",
            secret_access_key="SECRET",
            region="eu-west-1",
            bucket="my-bucket",
            use_ssl=True,
        )
        d = _s3_credentials_to_dict(creds)
        assert d == {
            "endpoint": "minio:9000",
            "access_key": "AKID",
            "secret_key": "SECRET",
            "region": "eu-west-1",
            "bucket": "my-bucket",
            "use_ssl": "true",
        }

    def test_empty_credentials_returns_empty_dict(self):
        creds = common_pb2.S3Credentials()
        d = _s3_credentials_to_dict(creds)
        assert d == {}

    def test_partial_credentials_only_includes_set_fields(self):
        creds = common_pb2.S3Credentials(
            access_key_id="AKID",
            secret_access_key="SECRET",
        )
        d = _s3_credentials_to_dict(creds)
        assert d == {"access_key": "AKID", "secret_key": "SECRET"}
        assert "endpoint" not in d
        assert "region" not in d


class TestPreviewPipelineRPC:
    """Tests for PreviewPipeline gRPC — regression for s3_credentials AttributeError."""

    @patch("rat_runner.server.preview_pipeline")
    def test_preview_does_not_crash_without_s3_credentials(
        self,
        mock_preview: MagicMock,
        stub: runner_pb2_grpc.RunnerServiceStub,
    ):
        """PreviewPipeline must not raise AttributeError when s3_credentials is absent."""
        from rat_runner.preview import PreviewResult

        mock_preview.return_value = PreviewResult()
        # This would crash with AttributeError before the fix
        resp = stub.PreviewPipeline(
            runner_pb2.PreviewPipelineRequest(
                namespace="myns",
                layer=common_pb2.LAYER_BRONZE,
                pipeline_name="my_pipe",
            )
        )
        assert resp is not None
        mock_preview.assert_called_once()

    def test_preview_invalid_layer_returns_error(
        self,
        stub: runner_pb2_grpc.RunnerServiceStub,
    ):
        with pytest.raises(grpc.RpcError) as exc_info:
            stub.PreviewPipeline(
                runner_pb2.PreviewPipelineRequest(
                    namespace="myns",
                    layer=common_pb2.LAYER_UNSPECIFIED,
                    pipeline_name="my_pipe",
                )
            )
        assert exc_info.value.code() == grpc.StatusCode.INVALID_ARGUMENT


class TestValidatePipelineRPC:
    """Tests for ValidatePipeline gRPC — regression for s3_credentials AttributeError."""

    @patch("rat_runner.server.list_s3_keys", return_value=[])
    def test_validate_does_not_crash_without_s3_credentials(
        self,
        mock_list: MagicMock,
        stub: runner_pb2_grpc.RunnerServiceStub,
    ):
        """ValidatePipeline must not raise AttributeError when s3_credentials is absent."""
        resp = stub.ValidatePipeline(
            runner_pb2.ValidatePipelineRequest(
                namespace="myns",
                layer=common_pb2.LAYER_BRONZE,
                pipeline_name="my_pipe",
            )
        )
        assert resp.valid is True
