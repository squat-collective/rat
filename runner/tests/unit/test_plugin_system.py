"""Tests for the plugin system — protocols, registry, and strategy dispatch."""

from __future__ import annotations

from typing import TYPE_CHECKING
from unittest.mock import MagicMock, patch

import pyarrow as pa

from rat_runner.plugin_protocols import (
    HookContext,
    HookProtocol,
    JinjaHelperProtocol,
    MergeStrategyProtocol,
    PipelineTypeProtocol,
    SourceConnectorProtocol,
)
from rat_runner.plugin_registry import (
    GROUP_HOOKS,
    GROUP_JINJA_HELPERS,
    GROUP_SOURCES,
    GROUP_STRATEGIES,
    PluginRegistry,
)
from rat_runner.strategies import (
    AppendOnlyStrategy,
    DeleteInsertStrategy,
    FullRefreshStrategy,
    IncrementalStrategy,
    SCD2Strategy,
    SnapshotStrategy,
)

if TYPE_CHECKING:
    import duckdb

    from rat_runner.config import NessieConfig, S3Config
    from rat_runner.models import PipelineConfig


# ── Protocol compliance tests ──────────────────────────────────────


class TestProtocolCompliance:
    """Verify built-in strategy classes satisfy MergeStrategyProtocol."""

    def test_full_refresh_implements_protocol(self):
        strategy = FullRefreshStrategy()
        assert isinstance(strategy, MergeStrategyProtocol)
        assert strategy.name == "full_refresh"

    def test_incremental_implements_protocol(self):
        strategy = IncrementalStrategy()
        assert isinstance(strategy, MergeStrategyProtocol)
        assert strategy.name == "incremental"

    def test_append_only_implements_protocol(self):
        strategy = AppendOnlyStrategy()
        assert isinstance(strategy, MergeStrategyProtocol)
        assert strategy.name == "append_only"

    def test_delete_insert_implements_protocol(self):
        strategy = DeleteInsertStrategy()
        assert isinstance(strategy, MergeStrategyProtocol)
        assert strategy.name == "delete_insert"

    def test_scd2_implements_protocol(self):
        strategy = SCD2Strategy()
        assert isinstance(strategy, MergeStrategyProtocol)
        assert strategy.name == "scd2"

    def test_snapshot_implements_protocol(self):
        strategy = SnapshotStrategy()
        assert isinstance(strategy, MergeStrategyProtocol)
        assert strategy.name == "snapshot"


# ── Registry discovery tests ───────────────────────────────────────


def _make_entry_point(name: str, cls: type):
    """Create a mock entry point that loads the given class."""
    ep = MagicMock()
    ep.name = name
    ep.load.return_value = cls
    return ep


class TestPluginRegistryDiscovery:
    """Tests for PluginRegistry.discover() with mocked entry_points."""

    @patch("rat_runner.plugin_registry.entry_points")
    def test_discovers_strategy_entry_points(self, mock_eps):
        mock_eps.return_value = [_make_entry_point("full_refresh", FullRefreshStrategy)]

        registry = PluginRegistry()
        registry.discover()

        assert registry.has_strategy("full_refresh")
        strategy = registry.get_strategy("full_refresh")
        assert strategy is not None
        assert strategy.name == "full_refresh"

    @patch("rat_runner.plugin_registry.entry_points")
    def test_discovers_multiple_strategies(self, mock_eps):
        def eps_side(group):
            if group == GROUP_STRATEGIES:
                return [
                    _make_entry_point("full_refresh", FullRefreshStrategy),
                    _make_entry_point("incremental", IncrementalStrategy),
                    _make_entry_point("append_only", AppendOnlyStrategy),
                ]
            return []

        mock_eps.side_effect = eps_side

        registry = PluginRegistry()
        registry.discover()

        assert set(registry.strategy_names()) == {"full_refresh", "incremental", "append_only"}

    @patch("rat_runner.plugin_registry.entry_points")
    def test_unknown_strategy_returns_none(self, mock_eps):
        mock_eps.return_value = []

        registry = PluginRegistry()
        registry.discover()

        assert registry.get_strategy("nonexistent") is None
        assert not registry.has_strategy("nonexistent")

    @patch("rat_runner.plugin_registry.entry_points")
    def test_skips_invalid_plugin(self, mock_eps):
        """Plugin that doesn't implement the protocol is skipped."""

        class NotAStrategy:
            pass

        mock_eps.return_value = [_make_entry_point("bad_strategy", NotAStrategy)]

        registry = PluginRegistry()
        registry.discover()

        assert not registry.has_strategy("bad_strategy")

    @patch("rat_runner.plugin_registry.entry_points")
    def test_skips_plugin_that_fails_to_load(self, mock_eps):
        """Plugin that raises on load is skipped."""
        ep = MagicMock()
        ep.name = "broken"
        ep.load.side_effect = ImportError("module not found")
        mock_eps.return_value = [ep]

        registry = PluginRegistry()
        registry.discover()

        assert not registry.has_strategy("broken")


# ── Strategy execution tests ───────────────────────────────────────


class TestStrategyExecution:
    """Tests for built-in strategy execute() methods."""

    @patch("rat_runner.strategies.write_iceberg", return_value=3)
    def test_full_refresh_delegates_to_write_iceberg(self, mock_write):
        strategy = FullRefreshStrategy()
        data = pa.table({"id": [1, 2, 3]})
        s3 = MagicMock()
        nessie = MagicMock()

        result = strategy.execute(data, "ns.layer.tbl", s3, nessie, "s3://loc", None)

        assert result == 3
        mock_write.assert_called_once()
        args = mock_write.call_args
        assert args[0][0] is data
        assert args[0][1] == "ns.layer.tbl"

    @patch("rat_runner.strategies.merge_iceberg", return_value=5)
    def test_incremental_delegates_to_merge_iceberg(self, mock_merge):
        strategy = IncrementalStrategy()
        data = pa.table({"id": [1]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ("id",)
        config.partition_by = None

        result = strategy.execute(data, "ns.layer.tbl", s3, nessie, "s3://loc", config)

        assert result == 5
        mock_merge.assert_called_once()

    @patch("rat_runner.strategies.write_iceberg", return_value=1)
    def test_incremental_without_unique_key_falls_back(self, mock_write):
        strategy = IncrementalStrategy()
        data = pa.table({"id": [1]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = None
        config.partition_by = None

        result = strategy.execute(data, "ns.layer.tbl", s3, nessie, "s3://loc", config)

        assert result == 1
        mock_write.assert_called_once()

    @patch("rat_runner.strategies.append_iceberg", return_value=2)
    def test_append_only_delegates_to_append_iceberg(self, mock_append):
        strategy = AppendOnlyStrategy()
        data = pa.table({"id": [1, 2]})
        s3 = MagicMock()
        nessie = MagicMock()

        result = strategy.execute(data, "ns.layer.tbl", s3, nessie, "s3://loc", None)

        assert result == 2
        mock_append.assert_called_once()

    @patch("rat_runner.strategies.delete_insert_iceberg", return_value=4)
    def test_delete_insert_delegates(self, mock_di):
        strategy = DeleteInsertStrategy()
        data = pa.table({"id": [1]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ("id",)
        config.partition_by = None

        result = strategy.execute(data, "ns.layer.tbl", s3, nessie, "s3://loc", config)

        assert result == 4
        mock_di.assert_called_once()

    @patch("rat_runner.strategies.scd2_iceberg", return_value=6)
    def test_scd2_delegates(self, mock_scd2):
        strategy = SCD2Strategy()
        data = pa.table({"id": [1]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ("id",)
        config.scd_valid_from = "valid_from"
        config.scd_valid_to = "valid_to"
        config.partition_by = None

        result = strategy.execute(data, "ns.layer.tbl", s3, nessie, "s3://loc", config)

        assert result == 6
        mock_scd2.assert_called_once()

    @patch("rat_runner.strategies.snapshot_iceberg", return_value=10)
    def test_snapshot_delegates(self, mock_snapshot):
        strategy = SnapshotStrategy()
        data = pa.table({"id": [1]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.partition_column = "date"
        config.partition_by = None

        result = strategy.execute(data, "ns.layer.tbl", s3, nessie, "s3://loc", config)

        assert result == 10
        mock_snapshot.assert_called_once()


# ── Hook dispatch tests ────────────────────────────────────────────


class _TestHook:
    """A simple hook for testing."""

    def __init__(self, phase: str):
        self._phase = phase
        self.called_with: list[HookContext] = []

    @property
    def phase(self):
        return self._phase

    def __call__(self, context: HookContext) -> None:
        self.called_with.append(context)


class TestHookDispatch:
    """Tests for hook registration and dispatch."""

    def test_dispatch_hooks_fires_registered_hooks(self):
        registry = PluginRegistry()
        hook = _TestHook("pre_execute")

        # Manually register a hook (simulating discovery)
        registry._register_hook("test_hook", hook)

        ctx = HookContext(
            namespace="ns",
            layer="silver",
            name="orders",
            run_id="r1",
            config=None,
            logger=MagicMock(),
        )
        registry.dispatch_hooks("pre_execute", ctx)

        assert len(hook.called_with) == 1
        assert hook.called_with[0].run_id == "r1"

    def test_dispatch_hooks_skips_different_phase(self):
        registry = PluginRegistry()
        hook = _TestHook("post_execute")

        registry._register_hook("test_hook", hook)

        ctx = HookContext(
            namespace="ns",
            layer="silver",
            name="orders",
            run_id="r1",
            config=None,
            logger=MagicMock(),
        )
        registry.dispatch_hooks("pre_execute", ctx)

        assert len(hook.called_with) == 0

    def test_dispatch_hooks_multiple_hooks_fire_in_order(self):
        registry = PluginRegistry()
        order: list[str] = []

        class OrderedHook:
            def __init__(self, name, phase):
                self._name = name
                self._phase = phase

            @property
            def phase(self):
                return self._phase

            def __call__(self, context):
                order.append(self._name)

        hook1 = OrderedHook("first", "pre_write")
        hook2 = OrderedHook("second", "pre_write")

        registry._register_hook("hook1", hook1)
        registry._register_hook("hook2", hook2)

        ctx = HookContext(
            namespace="ns",
            layer="silver",
            name="orders",
            run_id="r1",
            config=None,
            logger=MagicMock(),
        )
        registry.dispatch_hooks("pre_write", ctx)

        assert order == ["first", "second"]

    def test_dispatch_hooks_error_does_not_propagate(self):
        registry = PluginRegistry()

        class FailingHook:
            @property
            def phase(self):
                return "post_write"

            def __call__(self, context):
                raise RuntimeError("hook broke")

        registry._register_hook("bad_hook", FailingHook())

        ctx = HookContext(
            namespace="ns",
            layer="silver",
            name="orders",
            run_id="r1",
            config=None,
            logger=MagicMock(),
        )
        # Should not raise
        registry.dispatch_hooks("post_write", ctx)


# ── Jinja helper registration tests ───────────────────────────────


class TestJinjaHelperRegistration:
    """Tests for Jinja helper plugin registration in templates."""

    def test_registry_get_helpers_returns_dict(self):
        registry = PluginRegistry()
        assert registry.get_helpers() == {}

    def test_registry_stores_jinja_helpers(self):
        registry = PluginRegistry()

        class MyHelper:
            @property
            def name(self):
                return "my_func"

            def __call__(self, *args, **kwargs):
                return "hello"

        registry._register_jinja_helper("my_func", MyHelper())

        helpers = registry.get_helpers()
        assert "my_func" in helpers
        assert helpers["my_func"]() == "hello"

    @patch("rat_runner.templating._resolve_ref", return_value="iceberg_scan('fake')")
    def test_plugin_helpers_available_in_sql_template(self, mock_ref):
        """Plugin Jinja helpers are available as template variables."""
        from rat_runner.templating import compile_sql

        s3 = MagicMock()
        nessie = MagicMock()

        raw_sql = "SELECT {{ custom_fn() }} AS val"

        result = compile_sql(
            raw_sql,
            "ns",
            "silver",
            "test_pipe",
            s3,
            nessie,
            plugin_helpers={"custom_fn": lambda: 42},
        )

        assert "42" in result

    @patch("rat_runner.templating._resolve_ref", return_value="iceberg_scan('fake')")
    def test_plugin_helpers_do_not_override_builtins(self, mock_ref):
        """Plugin helpers cannot shadow built-in template variables."""
        from rat_runner.templating import compile_sql

        s3 = MagicMock()
        nessie = MagicMock()

        raw_sql = "SELECT {{ ref('silver.other') }} AS val"

        # Try to override 'ref' — should be ignored
        result = compile_sql(
            raw_sql,
            "ns",
            "silver",
            "test_pipe",
            s3,
            nessie,
            plugin_helpers={"ref": lambda x: "HACKED"},
        )

        # Built-in ref should still work
        assert "iceberg_scan" in result
        assert "HACKED" not in result


# ── Source connector tests ─────────────────────────────────────────


class TestSourceConnectorRegistry:
    """Tests for source connector registration and lookup."""

    def test_get_source_returns_none_for_unknown(self):
        registry = PluginRegistry()
        assert registry.get_source("postgres") is None

    def test_registers_and_retrieves_source(self):
        registry = PluginRegistry()

        class FakeSource:
            @property
            def name(self):
                return "postgres"

            def fetch(self, config, s3_config):
                return pa.table({"id": [1]})

        registry._register_source("postgres", FakeSource())

        source = registry.get_source("postgres")
        assert source is not None
        assert source.name == "postgres"
