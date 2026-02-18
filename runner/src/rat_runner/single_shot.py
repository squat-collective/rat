"""Single-shot pipeline execution mode for ContainerExecutor.

When RUN_MODE=single, the runner executes a single pipeline and exits.
Pipeline parameters come from environment variables, not gRPC.
Result is printed as a JSON line to stdout.
Exit code: 0 = success, 1 = failure.
"""

from __future__ import annotations

import json
import logging
import os
import sys

from rat_runner.config import NessieConfig, S3Config
from rat_runner.executor import execute_pipeline
from rat_runner.models import RunState, RunStatus

logger = logging.getLogger(__name__)


def run_single() -> None:
    """Execute a single pipeline run from environment variables, then exit."""
    run_id = os.environ.get("RUN_ID", "")
    namespace = os.environ.get("NAMESPACE", "")
    layer = os.environ.get("LAYER", "")
    pipeline_name = os.environ.get("PIPELINE_NAME", "")
    trigger = os.environ.get("TRIGGER", "manual")

    if not all([run_id, namespace, layer, pipeline_name]):
        _exit_error("Missing required env vars: RUN_ID, NAMESPACE, LAYER, PIPELINE_NAME")

    s3_config = S3Config.from_env()
    nessie_config = NessieConfig.from_env()

    run = RunState(
        run_id=run_id,
        namespace=namespace,
        layer=layer,
        pipeline_name=pipeline_name,
        trigger=trigger,
    )

    logger.info(
        "Single-shot mode: %s/%s/%s (run_id=%s, trigger=%s)",
        namespace,
        layer,
        pipeline_name,
        run_id,
        trigger,
    )

    execute_pipeline(run, s3_config, nessie_config)

    result = {
        "status": run.status.value,
        "rows_written": run.rows_written,
        "duration_ms": run.duration_ms,
    }
    if run.error:
        result["error"] = run.error

    # Print JSON result line to stdout for the ContainerExecutor to parse
    print(json.dumps(result), flush=True)

    if run.status == RunStatus.SUCCESS:
        sys.exit(0)
    else:
        sys.exit(1)


def _exit_error(message: str) -> None:
    """Print error result and exit with code 1."""
    result = {
        "status": "failed",
        "rows_written": 0,
        "duration_ms": 0,
        "error": message,
    }
    print(json.dumps(result), flush=True)
    logger.error(message)
    sys.exit(1)
