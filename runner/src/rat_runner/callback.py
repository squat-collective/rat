"""HTTP callback to push run status updates to ratd.

When RATD_CALLBACK_URL is set, the runner POSTs terminal status updates directly
to ratd instead of waiting for ratd to poll via GetRunStatus. This eliminates the
N-gRPC-calls-per-interval problem: 100 concurrent runs no longer generate 100
gRPC calls every 5 seconds. ratd's poll loop is reduced to a 60-second fallback
safety net for missed callbacks (e.g., network blips, runner crashes).

Endpoint: POST {RATD_CALLBACK_URL}/api/v1/internal/runs/{run_id}/status

The callback hits ratd's PRIVATE listener (default :8090, set via
INTERNAL_LISTEN_ADDR on the ratd container), NOT the public API listener
(:8080). The private listener has no authentication and must stay on the
container network. In docker-compose this is wired as
RATD_CALLBACK_URL=http://ratd:8090.

Payload: JSON with run_id, status, error, duration_ms, rows_written, archived_landing_zones
"""

from __future__ import annotations

import json
import logging
import os
import urllib.error
import urllib.request
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from rat_runner.models import RunState

logger = logging.getLogger(__name__)

# Base URL for ratd's PRIVATE listener. Example: "http://ratd:8090"
# (NOT the public 8080 — that listener does not expose the callback route.)
# When empty/unset, callbacks are disabled and ratd falls back to polling.
RATD_CALLBACK_URL = os.environ.get("RATD_CALLBACK_URL", "")


def notify_run_complete(run: RunState) -> None:
    """POST terminal run status to ratd's internal callback endpoint.

    Best-effort: failures are logged but never propagated. ratd's 60-second poll
    fallback will catch any missed callbacks. This is intentionally fire-and-forget
    because the runner should not block or fail on callback delivery.

    When the run carries a ``request_id`` (propagated from ratd via the
    SubmitPipeline gRPC metadata), it is echoed back as ``X-Request-ID`` on
    the outbound POST so ratd's chi RequestID middleware adopts the same ID
    and a single pipeline run can be grep'd across both services' logs.
    """
    # Local import to keep the test-time `extra=` annotations decoupled from
    # this module's import surface (avoids accidental top-level cycles when
    # subagents stub out callback for offline tests).
    from rat_runner.log import run_log_extras

    if not RATD_CALLBACK_URL:
        return

    if not run.is_terminal():
        logger.debug(
            "Skipping callback for non-terminal run (status=%s)",
            run.status.value,
            extra=run_log_extras(run),
        )
        return

    url = f"{RATD_CALLBACK_URL.rstrip('/')}/api/v1/internal/runs/{run.run_id}/status"

    payload = {
        "run_id": run.run_id,
        "status": run.status.value,
        "error": run.error or "",
        "duration_ms": run.duration_ms,
        "rows_written": run.rows_written,
        "archived_landing_zones": run.archived_zones or [],
    }

    headers: dict[str, str] = {"Content-Type": "application/json"}
    # Echo the originating request ID so ratd's RequestID middleware reuses
    # it instead of generating a fresh one for the callback HTTP request.
    if run.request_id:
        headers["X-Request-ID"] = run.request_id

    try:
        data = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(
            url,
            data=data,
            headers=headers,
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=5) as resp:
            logger.info(
                "Status callback sent (HTTP %d)",
                resp.status,
                extra={**run_log_extras(run), "status": run.status.value},
            )
    except urllib.error.URLError as e:
        logger.warning(
            "Status callback failed (ratd will poll as fallback): url=%s error=%s",
            url,
            e,
            extra=run_log_extras(run),
        )
    except Exception as e:
        logger.warning(
            "Status callback unexpected error: %s",
            e,
            extra=run_log_extras(run),
        )
