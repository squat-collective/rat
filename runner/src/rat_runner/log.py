"""Run logger — dual output to RunState deque and Python logging."""

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


class RunLogger:
    """Logger that writes to both a run's log deque (for gRPC StreamLogs)
    and Python's standard logging (for container stdout)."""

    def __init__(self, run: RunState) -> None:
        self._run = run

    def _log(self, level: str, message: str) -> None:
        self._run.add_log(level, message)
        py_level = _LEVEL_MAP.get(level, logging.INFO)
        logger.log(py_level, "[%s] %s", self._run.run_id, message)

    def debug(self, message: str) -> None:
        self._log("debug", message)

    def info(self, message: str) -> None:
        self._log("info", message)

    def warn(self, message: str) -> None:
        self._log("warn", message)

    def error(self, message: str) -> None:
        self._log("error", message)
