"""HTTP callback to push run status updates to ratd.

When RATD_CALLBACK_URL is set, the runner POSTs terminal status updates directly
to ratd instead of waiting for ratd to poll via GetRunStatus. This eliminates the
N-gRPC-calls-per-interval problem: 100 concurrent runs no longer generate 100
gRPC calls every 5 seconds. ratd's poll loop is reduced to a 60-second fallback
safety net for missed callbacks (e.g., network blips, runner crashes).

Endpoint: POST {RATD_CALLBACK_URL}/api/v1/internal/runs/{run_id}/status
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

# Base URL for ratd status callback. Example: "http://ratd:8080"
# When empty/unset, callbacks are disabled and ratd falls back to polling.
RATD_CALLBACK_URL = os.environ.get("RATD_CALLBACK_URL", "")


def notify_run_complete(run: RunState) -> None:
    """POST terminal run status to ratd's internal callback endpoint.

    Best-effort: failures are logged but never propagated. ratd's 60-second poll
    fallback will catch any missed callbacks. This is intentionally fire-and-forget
    because the runner should not block or fail on callback delivery.
    """
    if not RATD_CALLBACK_URL:
        return

    if not run.is_terminal():
        logger.debug(
            "Skipping callback for non-terminal run %s (status=%s)", run.run_id, run.status.value
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

    try:
        data = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(
            url,
            data=data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=5) as resp:
            logger.info(
                "Status callback sent: run=%s status=%s (HTTP %d)",
                run.run_id,
                run.status.value,
                resp.status,
            )
    except urllib.error.URLError as e:
        logger.warning(
            "Status callback failed (ratd will poll as fallback): run=%s url=%s error=%s",
            run.run_id,
            url,
            e,
        )
    except Exception as e:
        logger.warning(
            "Status callback unexpected error: run=%s error=%s",
            run.run_id,
            e,
        )
