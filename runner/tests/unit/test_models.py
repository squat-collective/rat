"""Tests for models — RunState, RunStatus, LogRecord, PipelineConfig."""

from __future__ import annotations

import threading

from rat_runner.models import (
    LogRecord,
    MergeStrategy,
    PipelineConfig,
    QualityTestResult,
    RunState,
    RunStatus,
)


class TestRunStatus:
    def test_pending_is_not_terminal(self):
        assert not RunStatus.PENDING.is_terminal()

    def test_running_is_not_terminal(self):
        assert not RunStatus.RUNNING.is_terminal()

    def test_success_is_terminal(self):
        assert RunStatus.SUCCESS.is_terminal()

    def test_failed_is_terminal(self):
        assert RunStatus.FAILED.is_terminal()

    def test_cancelled_is_terminal(self):
        assert RunStatus.CANCELLED.is_terminal()


class TestRunState:
    def test_initial_status_is_pending(self):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        assert run.status == RunStatus.PENDING
        assert run.rows_written == 0
        assert run.duration_ms == 0
        assert run.error == ""

    def test_add_log_appends_record(self):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        run.add_log("info", "hello")
        assert len(run.logs) == 1
        assert run.logs[0].level == "info"
        assert run.logs[0].message == "hello"

    def test_add_log_is_thread_safe(self):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        barrier = threading.Barrier(4)

        def writer(start: int):
            barrier.wait()
            for i in range(100):
                run.add_log("info", f"msg-{start + i}")

        threads = [threading.Thread(target=writer, args=(i * 100,)) for i in range(4)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        assert len(run.logs) == 400

    def test_log_deque_bounded(self):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        for i in range(15_000):
            run.add_log("info", f"msg-{i}")

        assert len(run.logs) == 10_000
        # Oldest entries were evicted
        assert run.logs[0].message == "msg-5000"

    def test_is_terminal_delegates_to_status(self):
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        assert not run.is_terminal()
        run.status = RunStatus.SUCCESS
        assert run.is_terminal()

    def test_add_log_notifies_wait_for_logs(self):
        """add_log wakes threads blocked in wait_for_logs."""
        import time

        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        woke_up = threading.Event()

        def waiter():
            run.wait_for_logs(timeout=5)
            woke_up.set()

        t = threading.Thread(target=waiter)
        t.start()

        # Give the waiter thread time to enter wait()
        time.sleep(0.1)

        before = time.time()
        run.add_log("info", "wake up")
        woke_up.wait(timeout=2)
        elapsed = time.time() - before

        t.join(timeout=2)
        assert woke_up.is_set(), "Waiter should have been woken by add_log"
        assert elapsed < 0.5, f"Wakeup took {elapsed:.3f}s — should be near-instant"

    def test_get_logs_from_returns_entries_after_cursor(self):
        """get_logs_from returns only entries from the given cursor position."""
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        run.add_log("info", "first")
        run.add_log("info", "second")
        run.add_log("info", "third")

        # From the start
        all_logs = run.get_logs_from(0)
        assert len(all_logs) == 3
        assert all_logs[0].message == "first"
        assert all_logs[2].message == "third"

        # From cursor=1 (skip first)
        tail = run.get_logs_from(1)
        assert len(tail) == 2
        assert tail[0].message == "second"
        assert tail[1].message == "third"

        # From cursor=3 (past the end)
        empty = run.get_logs_from(3)
        assert empty == []

    def test_get_logs_from_is_thread_safe(self):
        """get_logs_from can be called concurrently with add_log."""
        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        barrier = threading.Barrier(3)
        results: list[list] = [[] for _ in range(2)]

        def writer():
            barrier.wait()
            for i in range(50):
                run.add_log("info", f"msg-{i}")

        def reader(idx: int):
            barrier.wait()
            for _ in range(50):
                results[idx].append(len(run.get_logs_from(0)))

        threads = [
            threading.Thread(target=writer),
            threading.Thread(target=reader, args=(0,)),
            threading.Thread(target=reader, args=(1,)),
        ]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        # Readers should have seen monotonically non-decreasing counts
        for counts in results:
            for a, b in zip(counts, counts[1:], strict=False):
                assert a <= b, f"Log count went backwards: {a} > {b}"

    def test_wait_for_logs_returns_on_timeout(self):
        """wait_for_logs returns after the timeout even without new logs."""
        import time

        run = RunState(
            run_id="r1", namespace="ns", layer="silver", pipeline_name="orders", trigger="manual"
        )
        start = time.time()
        run.wait_for_logs(timeout=0.1)
        elapsed = time.time() - start
        # Should return after ~0.1s, give some slack
        assert elapsed < 0.5, f"wait_for_logs took {elapsed:.3f}s — expected ~0.1s"


class TestLogRecord:
    def test_fields(self):
        record = LogRecord(timestamp=1234567890.123, level="error", message="oops")
        assert record.timestamp == 1234567890.123
        assert record.level == "error"
        assert record.message == "oops"


class TestMergeStrategy:
    def test_has_six_members(self):
        assert len(MergeStrategy) == 6
        assert MergeStrategy.FULL_REFRESH == "full_refresh"
        assert MergeStrategy.INCREMENTAL == "incremental"
        assert MergeStrategy.APPEND_ONLY == "append_only"
        assert MergeStrategy.DELETE_INSERT == "delete_insert"
        assert MergeStrategy.SCD2 == "scd2"
        assert MergeStrategy.SNAPSHOT == "snapshot"

    def test_is_str_subclass(self):
        assert isinstance(MergeStrategy.FULL_REFRESH, str)

    def test_validate_valid(self):
        assert MergeStrategy.validate("full_refresh") is True
        assert MergeStrategy.validate("scd2") is True

    def test_validate_invalid(self):
        assert MergeStrategy.validate("unknown") is False
        assert MergeStrategy.validate("") is False


class TestPipelineConfig:
    def test_defaults(self):
        config = PipelineConfig()
        assert config.description == ""
        assert config.materialized == "table"
        assert config.unique_key == ()
        assert config.merge_strategy == MergeStrategy.FULL_REFRESH
        assert config.watermark_column == ""
        assert config.partition_column == ""
        assert config.scd_valid_from == "valid_from"
        assert config.scd_valid_to == "valid_to"

    def test_frozen(self):
        config = PipelineConfig(description="test")
        try:
            config.description = "changed"  # type: ignore[misc]
            assert False, "Should have raised"
        except AttributeError:
            pass

    def test_watermark_column(self):
        config = PipelineConfig(watermark_column="updated_at")
        assert config.watermark_column == "updated_at"

    def test_new_fields(self):
        config = PipelineConfig(
            partition_column="date",
            scd_valid_from="start_date",
            scd_valid_to="end_date",
        )
        assert config.partition_column == "date"
        assert config.scd_valid_from == "start_date"
        assert config.scd_valid_to == "end_date"


class TestQualityTestResult:
    def test_fields(self):
        result = QualityTestResult(
            test_name="not_null",
            test_file="ns/tests/quality/not_null.sql",
            severity="error",
            status="pass",
            row_count=0,
        )
        assert result.test_name == "not_null"
        assert result.severity == "error"
        assert result.status == "pass"
        assert result.row_count == 0
        assert result.message == ""
        assert result.duration_ms == 0

    def test_frozen(self):
        result = QualityTestResult(
            test_name="t",
            test_file="f",
            severity="error",
            status="pass",
            row_count=0,
        )
        try:
            result.status = "fail"  # type: ignore[misc]
            assert False, "Should have raised"
        except AttributeError:
            pass


class TestRunStateNewFields:
    def test_created_at_is_set(self):
        import time

        before = time.time()
        run = RunState(
            run_id="r1",
            namespace="ns",
            layer="silver",
            pipeline_name="orders",
            trigger="manual",
        )
        after = time.time()
        assert before <= run.created_at <= after

    def test_branch_default_empty(self):
        run = RunState(
            run_id="r1",
            namespace="ns",
            layer="silver",
            pipeline_name="orders",
            trigger="manual",
        )
        assert run.branch == ""

    def test_env_default_empty(self):
        run = RunState(
            run_id="r1",
            namespace="ns",
            layer="silver",
            pipeline_name="orders",
            trigger="manual",
        )
        assert run.env == {}

    def test_quality_results_default_empty(self):
        run = RunState(
            run_id="r1",
            namespace="ns",
            layer="silver",
            pipeline_name="orders",
            trigger="manual",
        )
        assert run.quality_results == []
