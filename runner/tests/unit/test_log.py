"""Tests for log — RunLogger dual output + JSON formatter."""

from __future__ import annotations

import asyncio
import io
import json
import logging

from rat_runner.json_log import (
    JSONFormatter,
    clear_run_context,
    configure_json_logging,
    set_run_context,
)
from rat_runner.log import RunLogger, run_log_extras
from rat_runner.models import RunState


class TestRunLogger:
    def test_writes_to_run_deque(self):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="p", trigger="manual"
        )
        log = RunLogger(run)
        log.info("hello")
        log.error("oops")

        assert len(run.logs) == 2
        assert run.logs[0].level == "info"
        assert run.logs[0].message == "hello"
        assert run.logs[1].level == "error"
        assert run.logs[1].message == "oops"

    def test_writes_to_python_logger(self, caplog: logging.LogCaptureFixture):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="p", trigger="manual"
        )
        log = RunLogger(run)

        with caplog.at_level(logging.DEBUG, logger="rat_runner.log"):
            log.debug("debug msg")
            log.info("info msg")
            log.warn("warn msg")
            log.error("error msg")

        messages = [r.message for r in caplog.records]
        assert any("debug msg" in m for m in messages)
        assert any("info msg" in m for m in messages)
        assert any("warn msg" in m for m in messages)
        assert any("error msg" in m for m in messages)

    def test_level_mapping(self, caplog: logging.LogCaptureFixture):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="p", trigger="manual"
        )
        log = RunLogger(run)

        with caplog.at_level(logging.DEBUG, logger="rat_runner.log"):
            log.debug("d")
            log.info("i")
            log.warn("w")
            log.error("e")

        levels = [r.levelno for r in caplog.records]
        assert logging.DEBUG in levels
        assert logging.INFO in levels
        assert logging.WARNING in levels
        assert logging.ERROR in levels

    def test_attaches_run_extras_to_python_logger(self, caplog: logging.LogCaptureFixture):
        """RunLogger forwards run_id/request_id/pipeline-key as `extra=` fields
        so the JSON formatter can promote them to top-level keys."""
        run = RunState(
            run_id="run-abc",
            namespace="underground",
            layer="bronze",
            pipeline_name="attendees",
            trigger="manual",
            request_id="req-xyz",
        )
        log = RunLogger(run)

        with caplog.at_level(logging.INFO, logger="rat_runner.log"):
            log.info("hello")

        assert len(caplog.records) == 1
        rec = caplog.records[0]
        # Every field needed for cross-service correlation lands on the record.
        assert rec.run_id == "run-abc"
        assert rec.request_id == "req-xyz"
        assert rec.namespace == "underground"
        assert rec.layer == "bronze"
        assert rec.pipeline_name == "attendees"


class TestRunLogExtras:
    def test_returns_all_correlation_fields(self):
        run = RunState(
            run_id="r1",
            namespace="ns",
            layer="silver",
            pipeline_name="p",
            trigger="manual",
            request_id="req-1",
        )
        extras = run_log_extras(run)
        assert extras == {
            "run_id": "r1",
            "request_id": "req-1",
            "namespace": "ns",
            "layer": "silver",
            "pipeline_name": "p",
        }

    def test_request_id_defaults_to_empty(self):
        run = RunState(
            run_id="r1",
            namespace="ns",
            layer="silver",
            pipeline_name="p",
            trigger="manual",
        )
        extras = run_log_extras(run)
        assert extras["request_id"] == ""


class TestJSONFormatter:
    def _format(self, level: int, msg: str, **extras: object) -> dict[str, object]:
        """Helper: build a LogRecord, format it, parse the JSON back."""
        record = logging.LogRecord(
            name="rat_runner.test",
            level=level,
            pathname=__file__,
            lineno=1,
            msg=msg,
            args=None,
            exc_info=None,
        )
        for key, value in extras.items():
            setattr(record, key, value)
        line = JSONFormatter().format(record)
        return json.loads(line)

    def test_emits_required_top_level_keys(self):
        out = self._format(logging.INFO, "hello world")
        assert out["msg"] == "hello world"
        assert out["level"] == "info"
        assert out["logger"] == "rat_runner.test"
        # Time must be RFC3339-ish with millisecond precision and UTC Z suffix.
        assert isinstance(out["time"], str)
        assert out["time"].endswith("Z")
        assert "T" in out["time"]

    def test_level_uses_slog_vocabulary(self):
        assert self._format(logging.DEBUG, "x")["level"] == "debug"
        assert self._format(logging.INFO, "x")["level"] == "info"
        assert self._format(logging.WARNING, "x")["level"] == "warn"
        assert self._format(logging.ERROR, "x")["level"] == "error"
        # CRITICAL collapses to "error" to match slog (which has no CRITICAL).
        assert self._format(logging.CRITICAL, "x")["level"] == "error"

    def test_promotes_extras_to_top_level_fields(self):
        """The whole point of the JSON formatter — `extra={…}` lands as
        top-level keys so grep'ing for run_id / request_id works."""
        out = self._format(
            logging.INFO,
            "Submitting pipeline",
            run_id="run-abc",
            request_id="req-xyz",
            namespace="underground",
            layer="bronze",
            pipeline_name="attendees",
        )
        assert out["run_id"] == "run-abc"
        assert out["request_id"] == "req-xyz"
        assert out["namespace"] == "underground"
        assert out["layer"] == "bronze"
        assert out["pipeline_name"] == "attendees"

    def test_skips_stdlib_reserved_attrs(self):
        """Process/thread/file noise must NOT appear in JSON output.

        Note: ``msg`` is intentionally a top-level key we DO emit (it's the
        formatted message). The "reserved" set means the formatter must not
        copy stdlib's *internal* attributes through to the JSON payload."""
        out = self._format(logging.INFO, "hi")
        for noise in ("threadName", "process", "pathname", "args", "levelno", "filename"):
            assert noise not in out, f"{noise!r} should not appear in JSON output"

    def test_each_line_is_a_single_json_object(self):
        """Container log aggregators expect JSONL — one object per line, no
        embedded newlines from the formatter."""
        record = logging.LogRecord(
            name="x",
            level=logging.INFO,
            pathname="x",
            lineno=1,
            msg="line\nwith\nnewlines",  # message itself can contain \n
            args=None,
            exc_info=None,
        )
        out = JSONFormatter().format(record)
        # Formatter must not emit a literal newline outside of the JSON-encoded
        # string payload.
        assert out.count("\n") == 0
        parsed = json.loads(out)
        assert parsed["msg"] == "line\nwith\nnewlines"


class TestConfigureJSONLogging:
    def test_installs_json_handler_on_root(self):
        try:
            configure_json_logging(level=logging.INFO)
            root = logging.getLogger()
            assert len(root.handlers) == 1
            assert isinstance(root.handlers[0].formatter, JSONFormatter)
        finally:
            # Reset to a sane state for other tests.
            root = logging.getLogger()
            for h in list(root.handlers):
                root.removeHandler(h)

    def test_idempotent_replaces_existing_handlers(self):
        try:
            configure_json_logging()
            configure_json_logging()  # should not double-up
            assert len(logging.getLogger().handlers) == 1
        finally:
            root = logging.getLogger()
            for h in list(root.handlers):
                root.removeHandler(h)

    def test_end_to_end_emits_jsonl_with_extras(self):
        """Configure root logger to write to a buffer and verify the line that
        emerges from a real logger.log call is parseable JSON with extras."""
        buffer = io.StringIO()
        handler = logging.StreamHandler(buffer)
        handler.setFormatter(JSONFormatter())
        root = logging.getLogger()
        # Save and replace existing handlers.
        prev = list(root.handlers)
        prev_level = root.level
        for h in prev:
            root.removeHandler(h)
        root.addHandler(handler)
        root.setLevel(logging.INFO)

        try:
            logging.getLogger("test").info(
                "Submitting pipeline",
                extra={"run_id": "r-1", "request_id": "q-9", "namespace": "n"},
            )
        finally:
            root.removeHandler(handler)
            root.setLevel(prev_level)
            for h in prev:
                root.addHandler(h)

        line = buffer.getvalue().strip()
        assert "\n" not in line  # exactly one line
        parsed = json.loads(line)
        assert parsed["msg"] == "Submitting pipeline"
        assert parsed["run_id"] == "r-1"
        assert parsed["request_id"] == "q-9"
        assert parsed["namespace"] == "n"


class TestRunContextVar:
    """Tests for the contextvars-bound run context that lets subsystem loggers
    (iceberg, nessie, maintenance, …) inherit run extras without having a
    RunState in scope."""

    def _capture(self, logger_name: str, msg: str, extras: dict | None = None) -> dict:
        """Drive a real LogRecord through JSONFormatter and parse the line.

        Important: we route through the actual ``logging`` module (not just
        ``JSONFormatter().format(record)``) so the extras-attached attributes
        land on the record exactly the way stdlib does it.
        """
        buffer = io.StringIO()
        handler = logging.StreamHandler(buffer)
        handler.setFormatter(JSONFormatter())
        target = logging.getLogger(logger_name)
        # Use a dedicated logger and isolate it from any root handlers so the
        # test is independent of root logger state.
        target.handlers = [handler]
        target.setLevel(logging.DEBUG)
        target.propagate = False
        try:
            if extras is not None:
                target.info(msg, extra=extras)
            else:
                target.info(msg)
        finally:
            target.handlers = []
            target.propagate = True
        return json.loads(buffer.getvalue().strip())

    def test_logs_inherit_set_run_context(self):
        """After binding context, a log from ANY module gets the extras —
        even if the caller forgets to pass extra= explicitly."""
        token = set_run_context(
            {
                "run_id": "abc",
                "request_id": "req-1",
                "namespace": "underground",
                "layer": "bronze",
                "pipeline_name": "attendees",
            }
        )
        try:
            # Use a module name that lives "outside" rat_runner.log — this
            # simulates iceberg.py / nessie.py emitting a raw line with no
            # per-call extras.
            out = self._capture("rat_runner.iceberg", "wrote partition")
        finally:
            clear_run_context(token)

        assert out["run_id"] == "abc"
        assert out["request_id"] == "req-1"
        assert out["namespace"] == "underground"
        assert out["layer"] == "bronze"
        assert out["pipeline_name"] == "attendees"
        assert out["logger"] == "rat_runner.iceberg"
        assert out["msg"] == "wrote partition"

    def test_per_call_extras_override_context(self):
        """Per-call extras must win on conflict — that's how a retry block can
        emit a line with the retry's run_id even while the outer context still
        holds the original."""
        token = set_run_context({"run_id": "abc", "namespace": "ns-a"})
        try:
            out = self._capture(
                "rat_runner.test",
                "retrying",
                extras={"run_id": "xyz"},
            )
        finally:
            clear_run_context(token)

        # Per-call wins on the conflicting key…
        assert out["run_id"] == "xyz"
        # …but non-conflicting context-bound keys are still present.
        assert out["namespace"] == "ns-a"

    def test_clear_run_context_drops_extras(self):
        """After clearing, subsequent logs must not include the previously
        bound extras — otherwise the next run on the same thread would
        inherit stale data."""
        token = set_run_context({"run_id": "abc"})
        clear_run_context(token)

        out = self._capture("rat_runner.test", "after clear")
        assert "run_id" not in out

    def test_contextvar_isolated_across_async_tasks(self):
        """Two concurrent asyncio tasks each set their own context; neither
        sees the other's bound extras. ContextVars are per-task by default —
        this guards against accidental cross-task leakage if the implementation
        ever switches to a module-level dict."""

        async def task(run_id: str, results: dict[str, str]) -> None:
            token = set_run_context({"run_id": run_id})
            try:
                # Yield to the scheduler so both tasks have their context set
                # concurrently — without this the awaits would serialize and
                # the test wouldn't actually prove isolation.
                await asyncio.sleep(0)
                out = self._capture("rat_runner.test", f"from {run_id}")
                results[run_id] = out.get("run_id", "<missing>")
            finally:
                clear_run_context(token)

        async def main() -> dict[str, str]:
            results: dict[str, str] = {}
            await asyncio.gather(task("alpha", results), task("beta", results))
            return results

        results = asyncio.run(main())
        assert results == {"alpha": "alpha", "beta": "beta"}
