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

import contextvars
import json
import logging
from datetime import UTC, datetime

# Per-thread / per-asyncio-task run context — merged into every log emit by
# JSONFormatter so subsystem modules (iceberg, nessie, maintenance, …) whose
# module-level loggers don't have a RunState in scope still produce log lines
# tagged with run_id/request_id/namespace/layer/pipeline_name.
#
# This is the same pattern used by sentry/datadog for request-scoped context:
# the executor binds a snapshot of run_log_extras at the top of each pipeline
# run; per-call extras passed to ``logger.log(..., extra=…)`` take precedence
# on conflict because they're applied AFTER the context dict in ``format``.
#
# Concurrency note: contextvars are per-thread by default. The runner's
# ThreadPoolExecutor wrapper sets the context INSIDE the worker thread (at the
# top of execute_pipeline) rather than relying on copy_context propagation,
# so each pipeline thread builds its own isolated context regardless of
# whatever the dispatcher thread had set.
_run_context: contextvars.ContextVar[dict[str, object]] = contextvars.ContextVar(
    "rat_run_ctx", default={}
)


def set_run_context(extras: dict[str, object]) -> contextvars.Token[dict[str, object]]:
    """Bind run-scoped extras into the current thread/task context.

    Returns a token that ``clear_run_context`` uses to restore the prior value
    — that's how nested binds (e.g. retries) compose without leaking state
    back to the dispatcher when the outer scope exits.
    """
    return _run_context.set(dict(extras))


def clear_run_context(token: contextvars.Token[dict[str, object]]) -> None:
    """Restore the prior run-context binding using the token from set_run_context."""
    _run_context.reset(token)


def current_run_context() -> dict[str, object]:
    """Return the current run-context snapshot. Public for tests/diagnostics."""
    return dict(_run_context.get())


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
        ts = (
            datetime.fromtimestamp(record.created, tz=UTC).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3]
            + "Z"
        )
        payload: dict[str, object] = {
            "time": ts,
            "level": _LEVEL_NAMES.get(record.levelno, record.levelname.lower()),
            "msg": record.getMessage(),
            "logger": record.name,
        }

        # Merge the per-thread / per-task run context FIRST so per-call
        # ``extra={...}`` keys can override on conflict (loop below). This is
        # how subsystem loggers (iceberg, nessie, maintenance, …) that don't
        # have a RunState in scope still emit lines tagged with run_id et al.
        context_extras = _run_context.get()
        if context_extras:
            payload.update(context_extras)

        # Copy any extras attached via ``extra={...}`` into the top-level dict.
        # We intentionally skip the stdlib's reserved attributes so we don't
        # leak process/thread/path noise into structured output. Per-call
        # extras intentionally win over the context-bound ones (e.g. a retry
        # block can override the bound ``run_id`` for a single line).
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
