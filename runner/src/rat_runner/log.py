"""Run logger — dual output to RunState deque and Python logging.

Every log line is emitted to two sinks:

  1. The run's bounded in-memory deque (consumed by gRPC StreamLogs).
  2. Python's stdlib ``logging`` module, which is configured at process start
     to use :class:`rat_runner.json_log.JSONFormatter` so each line appears on
     stdout as a single JSON object.

To make those JSON lines correlatable across services we attach the run's
identifying fields (``run_id``, ``request_id``, ``namespace``, ``layer``,
``pipeline_name``) as ``extra={...}`` on every call. The JSONFormatter then
promotes them to top-level keys so a single ``grep request_id=…`` against
both ratd and runner output returns the full story of one pipeline run.
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from rat_runner.models import RunState

logger = logging.getLogger(__name__)

_LEVEL_MAP = {
    "debug": logging.DEBUG,
    "info": logging.INFO,
    "warn": logging.WARNING,
    "error": logging.ERROR,
}


def run_log_extras(run: RunState) -> dict[str, str]:
    """Return the standard set of structured fields for a run.

    Designed to be merged into ``extra={...}`` on any ``logger.log`` call from
    code that has access to a :class:`RunState`. Keeping it as a free function
    means subsystems that don't carry a :class:`RunLogger` (e.g. server-side
    handlers that only have a :class:`RunState`) can still emit correlatable
    JSON lines via the stdlib logger.
    """
    return {
        "run_id": run.run_id,
        "request_id": run.request_id,
        "namespace": run.namespace,
        "layer": run.layer,
        "pipeline_name": run.pipeline_name,
    }


class RunLogger:
    """Logger that writes to both a run's log deque (for gRPC StreamLogs)
    and Python's standard logging (for container stdout)."""

    def __init__(self, run: RunState) -> None:
        self._run = run

    def _log(self, level: str, message: str) -> None:
        self._run.add_log(level, message)
        py_level = _LEVEL_MAP.get(level, logging.INFO)
        # The JSON formatter promotes every extras key to a top-level field,
        # so downstream tooling can filter on ``run_id``/``request_id`` etc.
        # We send the raw message (no ``[run_id]`` prefix) because that data
        # is already structured.
        logger.log(py_level, message, extra=run_log_extras(self._run))

    def debug(self, message: str) -> None:
        self._log("debug", message)

    def info(self, message: str) -> None:
        self._log("info", message)

    def warn(self, message: str) -> None:
        self._log("warn", message)

    def error(self, message: str) -> None:
        self._log("error", message)
