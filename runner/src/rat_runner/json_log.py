"""JSON log formatter — emits one JSON object per line on stdout.

Output format matches Go's `slog` JSON handler so ratd and runner logs can be
parsed by the same aggregator (and grep'd by shared keys like ``request_id``
and ``run_id``) without bespoke formatters per-service.

Example output::

    {"time":"2026-05-27T10:14:32.123Z","level":"info","msg":"Submitting pipeline",
     "logger":"rat_runner.server","run_id":"abc-123","request_id":"r-xyz",
     "namespace":"underground","layer":"bronze","pipeline_name":"attendees"}

Use :func:`configure_json_logging` at process start to install it on the root
logger. Anything passed through ``logging.LoggerAdapter`` or ``extra={}`` lands
as a top-level JSON field on the resulting line — that's how we propagate the
``request_id`` / ``run_id`` / pipeline-key context across every nested module.
"""

from __future__ import annotations

import json
import logging
from datetime import datetime, timezone

# Standard LogRecord attributes that should NOT be copied into the JSON payload
# as user "extras" — they're either already represented (level, time, msg) or
# noise (process internals).
_RESERVED_RECORD_ATTRS: frozenset[str] = frozenset(
    {
        "args",
        "asctime",
        "created",
        "exc_info",
        "exc_text",
        "filename",
        "funcName",
        "levelname",
        "levelno",
        "lineno",
        "message",
        "module",
        "msecs",
        "msg",
        "name",
        "pathname",
        "process",
        "processName",
        "relativeCreated",
        "stack_info",
        "thread",
        "threadName",
        "taskName",
    }
)

# Map stdlib log levels to lowercase strings matching slog's level vocabulary.
# (slog: "debug", "info", "warn", "error"; stdlib: "DEBUG", "INFO", "WARNING", "ERROR")
_LEVEL_NAMES: dict[int, str] = {
    logging.DEBUG: "debug",
    logging.INFO: "info",
    logging.WARNING: "warn",
    logging.ERROR: "error",
    logging.CRITICAL: "error",
}


class JSONFormatter(logging.Formatter):
    """Format each LogRecord as a single JSON object.

    Top-level keys: ``time`` (RFC3339 with millisecond precision, UTC),
    ``level`` (slog-style lowercase), ``msg`` (the formatted message),
    and ``logger`` (the python logger name).

    Any attribute set on the LogRecord via ``extra={...}`` (and not in the
    stdlib reserved set) becomes a top-level JSON key. This is the channel
    through which we emit ``run_id``, ``request_id``, ``namespace``, ``layer``
    and ``pipeline_name`` from the RunLogger.
    """

    def format(self, record: logging.LogRecord) -> str:
        # RFC3339 with millisecond precision in UTC — matches slog's default.
        ts = datetime.fromtimestamp(record.created, tz=timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%S.%f"
        )[:-3] + "Z"
        payload: dict[str, object] = {
            "time": ts,
            "level": _LEVEL_NAMES.get(record.levelno, record.levelname.lower()),
            "msg": record.getMessage(),
            "logger": record.name,
        }

        # Copy any extras attached via ``extra={...}`` into the top-level dict.
        # We intentionally skip the stdlib's reserved attributes so we don't
        # leak process/thread/path noise into structured output.
        for key, value in record.__dict__.items():
            if key in _RESERVED_RECORD_ATTRS or key.startswith("_"):
                continue
            payload[key] = value

        # Format exception info into a dedicated field if present.
        if record.exc_info:
            payload["exception"] = self.formatException(record.exc_info)

        # ``default=str`` keeps the formatter robust against non-JSON-native
        # values (UUIDs, Paths, Decimal, etc.) that callers may pass as extras.
        return json.dumps(payload, default=str, separators=(",", ":"))


def configure_json_logging(level: int = logging.INFO) -> None:
    """Install :class:`JSONFormatter` as the only handler on the root logger.

    Idempotent — repeated calls replace existing handlers so test fixtures can
    reset state. Targets stdout because container runtimes (Docker/Podman/k8s)
    capture stdout as the log stream.

    Note: stdlib's ``logging.basicConfig`` is a no-op if a handler is already
    present, which is why we manage the root handler list directly.
    """
    root = logging.getLogger()
    root.setLevel(level)
    # Replace any existing handlers — important so re-execs (plugin auto-install)
    # don't end up with duplicate stdout writers emitting every line twice.
    for handler in list(root.handlers):
        root.removeHandler(handler)
    handler = logging.StreamHandler()
    handler.setFormatter(JSONFormatter())
    root.addHandler(handler)
