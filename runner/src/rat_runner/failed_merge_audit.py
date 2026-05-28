"""HTTP audit for Phase 5 (branch merge) terminal failures.

When the runner cannot merge an ephemeral branch into main and has
exhausted its retry budget, it POSTs a record here so an operator can
recover the retained branch by hand. The endpoint lives on ratd's
PRIVATE listener (the same `RATD_CALLBACK_URL` base used by the
status callback).

Endpoint: POST {RATD_CALLBACK_URL}/api/v1/internal/failed-merges
Payload:  JSON {run_id, branch_name, source_hash, target_hash,
                error_kind, error_message}

This is best-effort. Failures are logged loudly but never re-raised:
the branch is already retained, the error is already in the runner
logs, and rejecting the run for an audit-side hiccup would be worse
than missing a database row.
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


def _ratd_url() -> str:
    """Read RATD_CALLBACK_URL fresh each call so tests can patch os.environ."""
    return os.environ.get("RATD_CALLBACK_URL", "")


def record_failed_merge(
    run: RunState,
    branch_name: str,
    source_hash: str | None,
    target_hash: str | None,
    error_kind: str,
    error_message: str,
) -> None:
    """POST a failed-merge audit record to ratd's internal endpoint.

    Best-effort: any failure is logged at WARNING but never raised.
    The retained branch + structured runner log are the real recovery
    surface; this audit row is the operator-facing index.

    `error_kind` is a short tag — e.g. "transient_exhausted",
    "conflict_exhausted", "permanent_4xx", "unknown".
    """
    from rat_runner.log import run_log_extras

    base = _ratd_url()
    if not base:
        # Without a configured ratd URL there is nowhere to send the
        # audit row. Log the full record so a human reading runner logs
        # can still see what happened.
        logger.warning(
            "RATD_CALLBACK_URL not set — failed_merges audit dropped",
            extra={
                **run_log_extras(run),
                "branch": branch_name,
                "error_kind": error_kind,
                "error_message": error_message,
                "merge_lost_data": True,
            },
        )
        return

    url = f"{base.rstrip('/')}/api/v1/internal/failed-merges"
    payload = {
        "run_id": run.run_id,
        "branch_name": branch_name,
        "source_hash": source_hash or "",
        "target_hash": target_hash or "",
        "error_kind": error_kind,
        "error_message": error_message,
    }
    headers: dict[str, str] = {"Content-Type": "application/json"}
    if run.request_id:
        headers["X-Request-ID"] = run.request_id

    try:
        data = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(url, data=data, headers=headers, method="POST")
        with urllib.request.urlopen(req, timeout=5) as resp:
            logger.info(
                "Failed-merge audit recorded (HTTP %d)",
                resp.status,
                extra={
                    **run_log_extras(run),
                    "branch": branch_name,
                    "error_kind": error_kind,
                },
            )
    except urllib.error.URLError as e:
        logger.warning(
            "Failed-merge audit POST failed (branch retained, manual recovery "
            "needed): url=%s error=%s",
            url,
            e,
            extra={
                **run_log_extras(run),
                "branch": branch_name,
                "error_kind": error_kind,
            },
        )
    except Exception as e:  # noqa: BLE001 — never let audit failures kill the runner
        logger.warning(
            "Failed-merge audit unexpected error: %s",
            e,
            extra={
                **run_log_extras(run),
                "branch": branch_name,
                "error_kind": error_kind,
            },
        )
