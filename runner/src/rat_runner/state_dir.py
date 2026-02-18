"""Crash recovery via JSON marker files.

When a run starts, a small JSON marker is written to a state directory.
When a run completes (success, failure, or cancellation), the marker is removed.
On startup, any remaining markers indicate runs that were in-flight when the
process crashed — these are reported so the server can register them as failed.

The state directory defaults to /tmp/rat-runner-state/ but can be overridden
via the RUNNER_STATE_DIR environment variable.
"""

from __future__ import annotations

import contextlib
import json
import logging
import os
from dataclasses import dataclass
from pathlib import Path

logger = logging.getLogger(__name__)

DEFAULT_STATE_DIR = "/tmp/rat-runner-state"


def get_state_dir() -> Path:
    """Return the configured state directory, creating it if necessary."""
    state_dir = Path(os.environ.get("RUNNER_STATE_DIR", DEFAULT_STATE_DIR))
    state_dir.mkdir(parents=True, exist_ok=True)
    return state_dir


@dataclass(frozen=True)
class CrashedRun:
    """Minimal info recovered from a marker file for a run that was in-flight
    when the runner process crashed."""

    run_id: str
    namespace: str
    layer: str
    pipeline_name: str
    trigger: str


def write_marker(
    state_dir: Path, run_id: str, namespace: str, layer: str, pipeline_name: str, trigger: str
) -> None:
    """Write a JSON marker file for an in-flight run."""
    marker = state_dir / f"{run_id}.json"
    data = {
        "run_id": run_id,
        "namespace": namespace,
        "layer": layer,
        "pipeline_name": pipeline_name,
        "trigger": trigger,
    }
    marker.write_text(json.dumps(data), encoding="utf-8")


def remove_marker(state_dir: Path, run_id: str) -> None:
    """Remove the marker file for a completed run. Best-effort — ignores missing files."""
    marker = state_dir / f"{run_id}.json"
    with contextlib.suppress(FileNotFoundError):
        marker.unlink()


def collect_crashed_runs(state_dir: Path) -> list[CrashedRun]:
    """Scan the state directory for leftover marker files.

    Each remaining marker represents a run that was in-flight when the runner
    process died. Returns the list of crashed runs and removes the marker files.
    """
    crashed: list[CrashedRun] = []

    if not state_dir.exists():
        return crashed

    for marker_path in sorted(state_dir.glob("*.json")):
        try:
            data = json.loads(marker_path.read_text(encoding="utf-8"))
            crashed.append(
                CrashedRun(
                    run_id=data["run_id"],
                    namespace=data["namespace"],
                    layer=data["layer"],
                    pipeline_name=data["pipeline_name"],
                    trigger=data["trigger"],
                )
            )
            marker_path.unlink()
        except (json.JSONDecodeError, KeyError, OSError) as exc:
            logger.warning("Ignoring corrupt marker file %s: %s", marker_path, exc)
            # Remove corrupt markers so they don't accumulate
            with contextlib.suppress(OSError):
                marker_path.unlink()

    return crashed
