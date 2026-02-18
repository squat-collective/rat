"""Tests for log — RunLogger dual output."""

from __future__ import annotations

import logging

from rat_runner.log import RunLogger
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
