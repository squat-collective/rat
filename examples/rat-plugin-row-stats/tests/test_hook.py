"""Tests for row-stats post-write hook."""

from __future__ import annotations

from unittest.mock import MagicMock

from rat_plugin_row_stats.hook import RowStatsHook


# ── Protocol compliance ───────────────────────────────────────────


class TestProtocolCompliance:
    def test_implements_hook_protocol(self):
        from rat_runner.plugin_protocols import HookProtocol

        hook = RowStatsHook()
        assert isinstance(hook, HookProtocol)
        assert hook.phase == "post_write"


# ── Log format tests ─────────────────────────────────────────────


def _make_context(
    *,
    namespace: str = "ns",
    layer: str = "silver",
    name: str = "orders",
    run_id: str = "r1",
    config: object | None = None,
    extra: dict | None = None,
) -> MagicMock:
    """Build a HookContext-compatible mock."""
    ctx = MagicMock()
    ctx.namespace = namespace
    ctx.layer = layer
    ctx.name = name
    ctx.run_id = run_id
    ctx.config = config
    ctx.extra = extra or {}
    ctx.logger = MagicMock()
    return ctx


class TestLogFormat:
    def test_logs_row_count_and_strategy(self):
        config = MagicMock()
        config.merge_strategy = "incremental"
        config.unique_key = ()

        ctx = _make_context(config=config, extra={"rows_written": 1500})
        hook = RowStatsHook()
        hook(ctx)

        ctx.logger.info.assert_called_once_with(
            "[row-stats] ns.silver.orders: wrote 1500 rows (strategy: incremental)"
        )

    def test_logs_unique_key_when_present(self):
        config = MagicMock()
        config.merge_strategy = "incremental"
        config.unique_key = ("id", "date")

        ctx = _make_context(config=config, extra={"rows_written": 100})
        hook = RowStatsHook()
        hook(ctx)

        calls = [call.args[0] for call in ctx.logger.info.call_args_list]
        assert "[row-stats] ns.silver.orders: wrote 100 rows (strategy: incremental)" in calls
        assert "[row-stats] unique_key: (id, date)" in calls

    def test_handles_config_none_gracefully(self):
        ctx = _make_context(config=None)
        hook = RowStatsHook()
        # Should not raise
        hook(ctx)

        ctx.logger.info.assert_called_once_with(
            "[row-stats] ns.silver.orders: wrote ? rows (strategy: unknown)"
        )

    def test_shows_question_mark_when_no_rows_in_extra(self):
        config = MagicMock()
        config.merge_strategy = "full_refresh"
        config.unique_key = ()

        ctx = _make_context(config=config)
        hook = RowStatsHook()
        hook(ctx)

        ctx.logger.info.assert_called_once_with(
            "[row-stats] ns.silver.orders: wrote ? rows (strategy: full_refresh)"
        )


# ── Error handling ────────────────────────────────────────────────


class TestErrorHandling:
    def test_errors_do_not_propagate(self):
        """Hook must swallow all errors — never crash the pipeline."""
        ctx = MagicMock()
        # Make context.namespace raise to simulate unexpected error
        type(ctx).namespace = property(lambda self: (_ for _ in ()).throw(RuntimeError("boom")))

        hook = RowStatsHook()
        # Should not raise
        hook(ctx)
