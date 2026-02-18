"""gRPC server — RunnerService implementation."""

from __future__ import annotations

import logging
import os
import re
import threading
import time
import uuid
from concurrent import futures
from typing import TYPE_CHECKING

import grpc
import pyarrow as pa

if TYPE_CHECKING:
    from collections.abc import Iterator
    from pathlib import Path

# Proto imports (gen/ must be on sys.path — see __main__.py)
from common.v1 import common_pb2  # type: ignore[import-untyped]
from google.protobuf import timestamp_pb2  # type: ignore[import-untyped]
from runner.v1 import (
    runner_pb2,  # type: ignore[import-untyped]
    runner_pb2_grpc,  # type: ignore[import-untyped]
)

from rat_runner.callback import notify_run_complete
from rat_runner.config import NessieConfig, S3Config, list_s3_keys, read_s3_text
from rat_runner.executor import execute_pipeline
from rat_runner.models import RunState, RunStatus
from rat_runner.preview import preview_pipeline
from rat_runner.state_dir import (
    collect_crashed_runs,
    get_state_dir,
    remove_marker,
    write_marker,
)
from rat_runner.templating import validate_template

logger = logging.getLogger(__name__)


def _sanitize_error(error: str) -> str:
    """Sanitize error messages before returning to clients.

    Strips DuckDB internals (file paths, memory addresses, stack traces) to avoid
    leaking server-side details. The full error is always logged server-side before
    calling this function.
    """
    # Remove absolute file paths (Unix and Windows style)
    sanitized = re.sub(r"(/[^\s:]+\.(?:py|so|cpp|c|h|hpp|o|parquet|csv|json))", "<path>", error)
    # Remove memory addresses (0x7fff...)
    sanitized = re.sub(r"0x[0-9a-fA-F]{6,}", "<addr>", sanitized)
    # Remove DuckDB C++ source references (e.g., "src/something.cpp:123")
    sanitized = re.sub(r"src/[^\s]+\.[ch]pp:\d+", "<internal>", sanitized)
    # Remove stack trace lines
    sanitized = re.sub(r"(?m)^\s*File \".*\", line \d+.*$", "", sanitized)
    sanitized = re.sub(r"(?m)^\s*at .*$", "", sanitized)
    # Collapse multiple blank lines
    sanitized = re.sub(r"\n{3,}", "\n\n", sanitized)
    return sanitized.strip()


# Env-configurable TTL for run cleanup (seconds)
RUN_TTL_SECONDS = int(os.environ.get("RUN_TTL_SECONDS", "3600"))

# Env-configurable max concurrent runs for backpressure.
# When the limit is reached, SubmitPipeline returns RESOURCE_EXHAUSTED
# so the platform can retry on the next scheduler tick instead of queuing unbounded.
MAX_CONCURRENT_RUNS = int(os.environ.get("RUNNER_MAX_CONCURRENT", "10"))

# Env-configurable gRPC server thread pool size.
# Controls how many gRPC requests the server can handle concurrently.
GRPC_MAX_WORKERS = int(os.environ.get("RUNNER_MAX_WORKERS", "10"))

# Proto Layer enum → string
_LAYER_MAP: dict[int, str] = {
    common_pb2.LAYER_BRONZE: "bronze",
    common_pb2.LAYER_SILVER: "silver",
    common_pb2.LAYER_GOLD: "gold",
}

# Internal RunStatus → proto RunStatus
_STATUS_TO_PROTO: dict[RunStatus, int] = {
    RunStatus.PENDING: common_pb2.RUN_STATUS_PENDING,
    RunStatus.RUNNING: common_pb2.RUN_STATUS_RUNNING,
    RunStatus.SUCCESS: common_pb2.RUN_STATUS_SUCCESS,
    RunStatus.FAILED: common_pb2.RUN_STATUS_FAILED,
    RunStatus.CANCELLED: common_pb2.RUN_STATUS_FAILED,  # proto has no CANCELLED
}


def _s3_credentials_to_dict(creds: common_pb2.S3Credentials) -> dict[str, str]:
    """Convert a proto S3Credentials message to a dict for S3Config.with_overrides()."""
    d: dict[str, str] = {}
    if creds.endpoint:
        d["endpoint"] = creds.endpoint
    if creds.access_key_id:
        d["access_key"] = creds.access_key_id
    if creds.secret_access_key:
        d["secret_key"] = creds.secret_access_key
    if creds.region:
        d["region"] = creds.region
    if creds.bucket:
        d["bucket"] = creds.bucket
    if creds.use_ssl:
        d["use_ssl"] = "true"
    return d


class RunnerServiceImpl(runner_pb2_grpc.RunnerServiceServicer):
    """gRPC RunnerService implementation.

    - SubmitPipeline: dispatch pipeline to thread pool, return run_id
    - GetRunStatus: read from in-memory run registry
    - StreamLogs: yield log entries from run's deque
    - CancelRun: set cancel_event on run
    """

    def __init__(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        max_workers: int = 4,
        state_dir: Path | None = None,
        max_concurrent_runs: int | None = None,
    ) -> None:
        self._s3_config = s3_config
        self._nessie_config = nessie_config
        self._runs: dict[str, RunState] = {}
        self._runs_lock = threading.Lock()
        self._max_concurrent_runs = (
            max_concurrent_runs if max_concurrent_runs is not None else MAX_CONCURRENT_RUNS
        )
        self._state_dir = state_dir if state_dir is not None else get_state_dir()
        self._pool = futures.ThreadPoolExecutor(max_workers=max_workers)
        self._cleanup_stop = threading.Event()
        self._cleanup_thread = threading.Thread(
            target=self._cleanup_loop, daemon=True, name="run-cleanup"
        )
        self._cleanup_thread.start()

        # Reconcile runs that were in-flight when the previous process crashed.
        self._reconcile_crashed_runs()

    def _cleanup_loop(self) -> None:
        """Periodically evict terminal runs older than RUN_TTL_SECONDS."""
        while not self._cleanup_stop.wait(timeout=60):
            self._evict_expired_runs()

    def _evict_expired_runs(self) -> None:
        """Remove terminal runs whose created_at is older than TTL."""
        now = time.time()
        with self._runs_lock:
            to_delete: list[str] = []
            for run_id, run in self._runs.items():
                if run.is_terminal() and (now - run.created_at) > RUN_TTL_SECONDS:
                    to_delete.append(run_id)
            for run_id in to_delete:
                del self._runs[run_id]
        if to_delete:
            logger.info("Cleanup: evicted %d expired run(s)", len(to_delete))

    def _reconcile_crashed_runs(self) -> None:
        """Check for marker files left by a previous crash and register them as failed.

        Any JSON marker still on disk represents a run that was in-flight when the
        runner process died. We create a terminal RunState for each so the platform
        can query their status via GetRunStatus and see the failure reason.
        """
        crashed = collect_crashed_runs(self._state_dir)
        if not crashed:
            return

        logger.warning(
            "Crash recovery: found %d in-flight run(s) from previous process",
            len(crashed),
        )
        with self._runs_lock:
            for cr in crashed:
                run = RunState(
                    run_id=cr.run_id,
                    namespace=cr.namespace,
                    layer=cr.layer,
                    pipeline_name=cr.pipeline_name,
                    trigger=cr.trigger,
                    status=RunStatus.FAILED,
                    error=(
                        "Runner process restarted"
                        " — run was in-flight when the previous process crashed"
                    ),
                )
                self._runs[cr.run_id] = run
                logger.warning(
                    "Crash recovery: marked run %s (%s.%s.%s) as FAILED",
                    cr.run_id,
                    cr.namespace,
                    cr.layer,
                    cr.pipeline_name,
                )

    def _execute_with_marker(
        self,
        run: RunState,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        published_versions: dict[str, str] | None,
    ) -> None:
        """Wrapper around execute_pipeline that manages marker file lifecycle and retries.

        Writes a marker before execution starts and removes it when execution
        finishes (regardless of outcome). If the process crashes mid-execution,
        the marker remains on disk and will be picked up by _reconcile_crashed_runs
        on the next startup.

        After execution completes, sends a status callback to ratd so it can
        update Postgres immediately instead of waiting for the next poll cycle.

        Retry policy: if the pipeline's config.yaml specifies max_retries > 0 and
        the run fails (not cancelled), retries up to max_retries times with
        retry_delay_seconds between attempts. The retry config is read from S3 once
        before the first attempt. Retries reset the run state to RUNNING before
        re-executing.
        """
        try:
            execute_pipeline(run, s3_config, nessie_config, published_versions)

            # Retry on failure if the pipeline config enables it.
            # We read config lazily after the first attempt since the executor
            # has already parsed it — but retry config lives in config.yaml which
            # we read here independently to keep the retry loop at the server layer.
            if run.status == RunStatus.FAILED:
                self._maybe_retry(run, s3_config, nessie_config, published_versions)
        finally:
            remove_marker(self._state_dir, run.run_id)
            # Push status to ratd (best-effort — ratd polls as fallback)
            notify_run_complete(run)

    def _maybe_retry(
        self,
        run: RunState,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        published_versions: dict[str, str] | None,
    ) -> None:
        """Retry a failed pipeline run if config.yaml specifies max_retries > 0.

        Reads the pipeline's config.yaml from S3 to determine retry settings.
        Resets run state between attempts so the executor starts fresh.
        Respects the cancel_event to allow cancellation during retry delay.
        """
        from rat_runner.config import parse_pipeline_config

        ns, layer, name = run.namespace, run.layer, run.pipeline_name
        config_key = f"{ns}/pipelines/{layer}/{name}/config.yaml"
        config_yaml = read_s3_text(s3_config, config_key)
        if not config_yaml:
            return

        try:
            config = parse_pipeline_config(config_yaml)
        except Exception:
            return

        if config.max_retries <= 0:
            return

        for attempt in range(1, config.max_retries + 1):
            logger.info(
                "Retry %d/%d for run %s (%s.%s.%s) after %ds delay",
                attempt,
                config.max_retries,
                run.run_id,
                ns,
                layer,
                name,
                config.retry_delay_seconds,
            )
            run.add_log(
                "info",
                f"Retrying ({attempt}/{config.max_retries}) after {config.retry_delay_seconds}s...",
            )

            # Wait for delay, but respect cancellation.
            if run.cancel_event.wait(timeout=config.retry_delay_seconds):
                run.add_log("warn", "Retry cancelled by user")
                return

            # Reset run state for retry.
            run.status = RunStatus.RUNNING
            run.error = ""
            run.rows_written = 0
            run.duration_ms = 0

            execute_pipeline(run, s3_config, nessie_config, published_versions)

            if run.status != RunStatus.FAILED:
                if run.status == RunStatus.SUCCESS:
                    run.add_log("info", f"Pipeline succeeded on retry {attempt}")
                return

            run.add_log("warn", f"Retry {attempt}/{config.max_retries} failed: {run.error}")

        logger.warning(
            "All %d retries exhausted for run %s",
            config.max_retries,
            run.run_id,
        )

    def SubmitPipeline(  # noqa: N802
        self,
        request: runner_pb2.SubmitPipelineRequest,
        context: grpc.ServicerContext,
    ) -> runner_pb2.SubmitPipelineResponse:
        layer_str = _LAYER_MAP.get(request.layer)
        if layer_str is None:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details(f"Invalid layer: {request.layer}")
            return runner_pb2.SubmitPipelineResponse()

        # Use platform-assigned run_id if provided (keeps archive folder names in sync)
        run_id = request.run_id if request.run_id else str(uuid.uuid4())

        # Per-run S3 config overrides (STS credentials)
        s3_config = self._s3_config
        if request.HasField("s3_credentials"):
            s3_config = self._s3_config.with_overrides(
                _s3_credentials_to_dict(request.s3_credentials)
            )

        # Per-run env vars
        env: dict[str, str] = {}
        if hasattr(request, "env") and request.env:
            env = dict(request.env)

        run = RunState(
            run_id=run_id,
            namespace=request.namespace,
            layer=layer_str,
            pipeline_name=request.pipeline_name,
            trigger=request.trigger,
            env=env,
        )

        # Backpressure: reject submission when at capacity so the platform
        # can retry on the next scheduler tick instead of queuing unbounded.
        # The capacity check and dict insertion are atomic under the lock to
        # prevent two concurrent submits from both passing the check.
        with self._runs_lock:
            active_count = sum(1 for r in self._runs.values() if not r.is_terminal())
            if active_count >= self._max_concurrent_runs:
                context.set_code(grpc.StatusCode.RESOURCE_EXHAUSTED)
                context.set_details(
                    f"Runner at capacity: {active_count}"
                    f"/{self._max_concurrent_runs} concurrent runs"
                )
                return runner_pb2.SubmitPipelineResponse()
            self._runs[run_id] = run

        logger.info(
            "Submitting pipeline: %s.%s.%s (run=%s, trigger=%s)",
            request.namespace,
            layer_str,
            request.pipeline_name,
            run_id,
            request.trigger,
        )

        # Write marker file before dispatching — if the process crashes during
        # execution, the marker will survive and be picked up on next startup.
        write_marker(
            self._state_dir,
            run_id,
            run.namespace,
            run.layer,
            run.pipeline_name,
            run.trigger,
        )

        # Extract published versions for versioned reads
        published_versions = (
            dict(request.published_versions) if request.published_versions else None
        )

        self._pool.submit(
            self._execute_with_marker,
            run,
            s3_config,
            self._nessie_config,
            published_versions,
        )

        return runner_pb2.SubmitPipelineResponse(
            run_id=run_id,
            status=common_pb2.RUN_STATUS_PENDING,
        )

    def GetRunStatus(  # noqa: N802
        self,
        request: common_pb2.GetRunStatusRequest,
        context: grpc.ServicerContext,
    ) -> common_pb2.GetRunStatusResponse:
        with self._runs_lock:
            run = self._runs.get(request.run_id)
        if run is None:
            context.set_code(grpc.StatusCode.NOT_FOUND)
            context.set_details(f"Run not found: {request.run_id}")
            return common_pb2.GetRunStatusResponse()

        # Sanitize error messages before returning — log full error server-side.
        error_msg = run.error
        if error_msg:
            logger.debug("Full run error for %s: %s", run.run_id, error_msg)
            error_msg = _sanitize_error(error_msg)

        return common_pb2.GetRunStatusResponse(
            run_id=run.run_id,
            status=_STATUS_TO_PROTO.get(run.status, common_pb2.RUN_STATUS_PENDING),
            rows_written=run.rows_written,
            duration_ms=run.duration_ms,
            error=error_msg,
            archived_landing_zones=run.archived_zones,
        )

    def StreamLogs(  # noqa: N802
        self,
        request: common_pb2.StreamLogsRequest,
        context: grpc.ServicerContext,
    ) -> Iterator[common_pb2.LogEntry]:
        with self._runs_lock:
            run = self._runs.get(request.run_id)
        if run is None:
            context.set_code(grpc.StatusCode.NOT_FOUND)
            context.set_details(f"Run not found: {request.run_id}")
            return

        cursor = 0
        while True:
            # Read only NEW entries from cursor (avoids copying entire deque)
            new_entries = run.get_logs_from(cursor)

            for record in new_entries:
                cursor += 1
                secs = int(record.timestamp)
                nanos = int((record.timestamp - secs) * 1_000_000_000)
                yield common_pb2.LogEntry(
                    timestamp=timestamp_pb2.Timestamp(seconds=secs, nanos=nanos),
                    level=record.level,
                    message=record.message,
                )

            # If not following or run is terminal, stop
            if not request.follow or run.is_terminal():
                break

            # Wait for new log entries (or timeout to recheck terminal status)
            run.wait_for_logs(timeout=1.0)

    def CancelRun(  # noqa: N802
        self,
        request: common_pb2.CancelRunRequest,
        context: grpc.ServicerContext,
    ) -> common_pb2.CancelRunResponse:
        with self._runs_lock:
            run = self._runs.get(request.run_id)
        if run is None:
            context.set_code(grpc.StatusCode.NOT_FOUND)
            context.set_details(f"Run not found: {request.run_id}")
            return common_pb2.CancelRunResponse(cancelled=False)

        if run.is_terminal():
            return common_pb2.CancelRunResponse(cancelled=False)

        run.cancel_event.set()
        return common_pb2.CancelRunResponse(cancelled=True)

    def PreviewPipeline(  # noqa: N802
        self,
        request: runner_pb2.PreviewPipelineRequest,
        context: grpc.ServicerContext,
    ) -> runner_pb2.PreviewPipelineResponse:
        layer_str = _LAYER_MAP.get(request.layer)
        if layer_str is None:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details(f"Invalid layer: {request.layer}")
            return runner_pb2.PreviewPipelineResponse()

        # Per-run S3 config overrides
        s3_config = self._s3_config
        if request.HasField("s3_credentials"):
            s3_config = self._s3_config.with_overrides(
                _s3_credentials_to_dict(request.s3_credentials)
            )

        preview_limit = request.preview_limit if request.preview_limit > 0 else 100

        logger.info(
            "Preview: %s.%s.%s (limit=%d)",
            request.namespace,
            layer_str,
            request.pipeline_name,
            preview_limit,
        )

        # Inline code for unsaved preview (skip S3 reads)
        code = request.code if request.code else None
        pipeline_type_hint = request.pipeline_type if request.pipeline_type else None

        result = preview_pipeline(
            namespace=request.namespace,
            layer=layer_str,
            pipeline_name=request.pipeline_name,
            s3_config=s3_config,
            nessie_config=self._nessie_config,
            preview_limit=preview_limit,
            code=code,
            pipeline_type=pipeline_type_hint,
        )

        # Serialize Arrow table to IPC bytes
        arrow_ipc = b""
        if result.arrow_table is not None and result.arrow_table.num_rows > 0:
            sink = pa.BufferOutputStream()
            writer = pa.ipc.new_stream(sink, result.arrow_table.schema)
            writer.write_table(result.arrow_table)
            writer.close()
            arrow_ipc = sink.getvalue().to_pybytes()

        # Build proto response
        columns = [runner_pb2.ColumnInfo(name=c.name, type=c.type) for c in result.columns]
        phases = [
            runner_pb2.PhaseProfile(
                name=p.name,
                duration_ms=p.duration_ms,
                metadata=p.metadata,
            )
            for p in result.phases
        ]
        logs = []
        for rec in result.logs:
            secs = int(rec.timestamp)
            nanos = int((rec.timestamp - secs) * 1_000_000_000)
            logs.append(
                common_pb2.LogEntry(
                    timestamp=timestamp_pb2.Timestamp(seconds=secs, nanos=nanos),
                    level=rec.level,
                    message=rec.message,
                )
            )

        # Sanitize error messages before returning.
        # Full error is already logged by preview_pipeline.
        preview_error = result.error
        if preview_error:
            preview_error = _sanitize_error(preview_error)

        return runner_pb2.PreviewPipelineResponse(
            arrow_ipc=arrow_ipc,
            columns=columns,
            total_row_count=result.total_row_count,
            phases=phases,
            explain_output=result.explain_output,
            memory_peak_bytes=result.memory_peak_bytes,
            logs=logs,
            error=preview_error,
            warnings=result.warnings,
        )

    def ValidatePipeline(  # noqa: N802
        self,
        request: runner_pb2.ValidatePipelineRequest,
        context: grpc.ServicerContext,
    ) -> runner_pb2.ValidatePipelineResponse:
        layer_str = _LAYER_MAP.get(request.layer)
        if layer_str is None:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details(f"Invalid layer: {request.layer}")
            return runner_pb2.ValidatePipelineResponse()

        # Per-request S3 config overrides
        s3_config = self._s3_config
        if request.HasField("s3_credentials"):
            s3_config = self._s3_config.with_overrides(
                _s3_credentials_to_dict(request.s3_credentials)
            )

        # List all .sql files under the pipeline prefix
        prefix = f"{request.namespace}/pipelines/{layer_str}/{request.pipeline_name}/"
        keys = list_s3_keys(s3_config, prefix, suffix=".sql")

        logger.info(
            "Validate: %s.%s.%s (%d files)",
            request.namespace,
            layer_str,
            request.pipeline_name,
            len(keys),
        )

        all_valid = True
        file_validations: list[runner_pb2.FileValidation] = []

        for key in keys:
            raw_sql = read_s3_text(s3_config, key)
            if raw_sql is None:
                continue

            errors, warnings = validate_template(raw_sql)
            file_valid = len(errors) == 0
            if not file_valid:
                all_valid = False

            file_validations.append(
                runner_pb2.FileValidation(
                    path=key,
                    valid=file_valid,
                    errors=errors,
                    warnings=warnings,
                )
            )

        return runner_pb2.ValidatePipelineResponse(
            valid=all_valid,
            files=file_validations,
        )

    @property
    def active_run_count(self) -> int:
        """Return the number of non-terminal runs currently tracked."""
        with self._runs_lock:
            return sum(1 for r in self._runs.values() if not r.is_terminal())

    def shutdown(self) -> None:
        """Cancel all active runs, stop cleanup thread, and shut down the thread pool."""
        self._cleanup_stop.set()
        self._cleanup_thread.join(timeout=5.0)
        with self._runs_lock:
            runs_snapshot = list(self._runs.values())
        for run in runs_snapshot:
            if not run.is_terminal():
                run.cancel_event.set()
        self._pool.shutdown(wait=True)


def _configure_server_port(server: grpc.Server, port: int) -> None:
    """Configure gRPC server port with optional TLS.

    Reads GRPC_TLS_CERT and GRPC_TLS_KEY env vars (file paths to PEM cert/key).
    If both are set, enables TLS via ssl_server_credentials.
    If neither is set, falls back to insecure port (backward compatible).
    Raises ValueError if only one of the two is set.
    """
    cert_path = os.environ.get("GRPC_TLS_CERT", "")
    key_path = os.environ.get("GRPC_TLS_KEY", "")

    if cert_path and key_path:
        with open(cert_path, "rb") as f:
            cert = f.read()
        with open(key_path, "rb") as f:
            key = f.read()
        creds = grpc.ssl_server_credentials([(key, cert)])
        server.add_secure_port(f"[::]:{port}", creds)
        logger.info("gRPC server listening on port %d (TLS enabled)", port)
    elif cert_path or key_path:
        raise ValueError("Both GRPC_TLS_CERT and GRPC_TLS_KEY must be set for TLS")
    else:
        server.add_insecure_port(f"[::]:{port}")
        logger.info("gRPC server listening on port %d (insecure)", port)


def serve(port: int = 50052) -> None:
    """Start the gRPC server."""
    s3_config = S3Config.from_env()
    nessie_config = NessieConfig.from_env()

    servicer = RunnerServiceImpl(s3_config, nessie_config)
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=GRPC_MAX_WORKERS))
    runner_pb2_grpc.add_RunnerServiceServicer_to_server(servicer, server)

    _configure_server_port(server, port)
    server.start()

    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        logger.info("Shutting down...")
    finally:
        servicer.shutdown()
        server.stop(grace=5)
