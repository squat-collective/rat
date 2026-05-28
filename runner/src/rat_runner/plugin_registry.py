"""Plugin registry — discovers and manages runner extension points.

Uses Python's importlib.metadata.entry_points() for plugin discovery.
Each run creates a fresh PluginRegistry and calls discover() to pick up
any installed entry points.

Extension groups:
  rat.strategies      → MergeStrategyProtocol
  rat.pipeline_types  → PipelineTypeProtocol
  rat.jinja_helpers   → JinjaHelperProtocol
  rat.hooks           → HookProtocol
  rat.sources         → SourceConnectorProtocol
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from importlib.metadata import entry_points

from rat_runner.plugin_protocols import (
    HookContext,
    HookPhase,
    HookProtocol,
    JinjaHelperProtocol,
    MergeStrategyProtocol,
    PipelineTypeProtocol,
    SourceConnectorProtocol,
)

logger = logging.getLogger(__name__)

# Entry point group names.
GROUP_STRATEGIES = "rat.strategies"
GROUP_PIPELINE_TYPES = "rat.pipeline_types"
GROUP_JINJA_HELPERS = "rat.jinja_helpers"
GROUP_HOOKS = "rat.hooks"
GROUP_SOURCES = "rat.sources"


@dataclass(frozen=True)
class PluginInfo:
    """Metadata for a discovered runner plugin entry point."""

    name: str  # entry point name ("soft_delete", "env_var")
    group: str  # "rat.strategies", "rat.hooks", etc.
    version: str  # package version
    package_name: str  # Python package name ("rat-plugin-soft-delete")


class PluginRegistry:
    """Discovers and holds all runner plugins for a single pipeline run.

    Usage:
        registry = PluginRegistry()
        registry.discover()
        strategy = registry.get_strategy("full_refresh")
    """

    def __init__(self) -> None:
        self._strategies: dict[str, MergeStrategyProtocol] = {}
        self._pipeline_types: dict[str, PipelineTypeProtocol] = {}
        self._jinja_helpers: dict[str, JinjaHelperProtocol] = {}
        self._hooks: dict[HookPhase, list[HookProtocol]] = {}
        self._sources: dict[str, SourceConnectorProtocol] = {}
        self._discovered_plugins: list[PluginInfo] = []

    def discover(self) -> None:
        """Scan installed entry points and load all plugins.

        Each entry point is loaded and validated against its protocol.
        Invalid plugins are logged and skipped.
        """
        self._discover_group(GROUP_STRATEGIES, MergeStrategyProtocol, self._register_strategy)
        self._discover_group(
            GROUP_PIPELINE_TYPES, PipelineTypeProtocol, self._register_pipeline_type
        )
        self._discover_group(GROUP_JINJA_HELPERS, JinjaHelperProtocol, self._register_jinja_helper)
        self._discover_group(GROUP_HOOKS, HookProtocol, self._register_hook)
        self._discover_group(GROUP_SOURCES, SourceConnectorProtocol, self._register_source)

        total = (
            len(self._strategies)
            + len(self._pipeline_types)
            + len(self._jinja_helpers)
            + sum(len(v) for v in self._hooks.values())
            + len(self._sources)
        )
        if total > 0:
            logger.info(
                "Plugin discovery complete: %d strategies, %d pipeline types, "
                "%d jinja helpers, %d hooks, %d sources",
                len(self._strategies),
                len(self._pipeline_types),
                len(self._jinja_helpers),
                sum(len(v) for v in self._hooks.values()),
                len(self._sources),
            )

    def _discover_group(
        self,
        group: str,
        protocol: type,
        register_fn: object,
    ) -> None:
        """Load entry points for a single group and validate each against its protocol."""
        eps = entry_points(group=group)
        for ep in eps:
            try:
                plugin_cls = ep.load()
                # Entry point may be a class (instantiate) or an instance.
                plugin = plugin_cls() if isinstance(plugin_cls, type) else plugin_cls
                if not isinstance(plugin, protocol):
                    logger.warning(
                        "Plugin '%s' from group '%s' does not implement %s, skipping",
                        ep.name,
                        group,
                        protocol.__name__,
                    )
                    continue
                register_fn(ep.name, plugin)  # type: ignore[operator]

                # Record metadata for list_plugins().
                dist = ep.dist
                self._discovered_plugins.append(
                    PluginInfo(
                        name=ep.name,
                        group=group,
                        version=dist.version if dist else "unknown",
                        package_name=dist.name if dist else "unknown",
                    )
                )
            except Exception:
                logger.warning(
                    "Failed to load plugin '%s' from group '%s'",
                    ep.name,
                    group,
                    exc_info=True,
                )

    def _register_strategy(self, name: str, plugin: MergeStrategyProtocol) -> None:
        self._strategies[name] = plugin
        logger.debug("Registered strategy: %s", name)

    def _register_pipeline_type(self, name: str, plugin: PipelineTypeProtocol) -> None:
        self._pipeline_types[name] = plugin
        logger.debug("Registered pipeline type: %s", name)

    def _register_jinja_helper(self, name: str, plugin: JinjaHelperProtocol) -> None:
        self._jinja_helpers[name] = plugin
        logger.debug("Registered jinja helper: %s", name)

    def _register_hook(self, name: str, plugin: HookProtocol) -> None:
        phase = plugin.phase
        if phase not in self._hooks:
            self._hooks[phase] = []
        self._hooks[phase].append(plugin)
        logger.debug("Registered hook: %s (phase: %s)", name, phase)

    def _register_source(self, name: str, plugin: SourceConnectorProtocol) -> None:
        self._sources[name] = plugin
        logger.debug("Registered source connector: %s", name)

    # ── Public accessors ───────────────────────────────────────────────

    def list_plugins(self) -> list[PluginInfo]:
        """Return metadata for all discovered plugins across all groups."""
        return list(self._discovered_plugins)

    def get_strategy(self, name: str) -> MergeStrategyProtocol | None:
        """Get a merge strategy by name, or None if not found."""
        return self._strategies.get(name)

    def get_pipeline_type(self, name: str) -> PipelineTypeProtocol | None:
        """Get a pipeline type by name, or None if not found."""
        return self._pipeline_types.get(name)

    def get_helpers(self) -> dict[str, JinjaHelperProtocol]:
        """Get all registered Jinja helpers as a name→callable dict."""
        return dict(self._jinja_helpers)

    def get_hooks(self, phase: HookPhase) -> list[HookProtocol]:
        """Get all hooks registered for a specific phase."""
        return list(self._hooks.get(phase, []))

    def get_source(self, name: str) -> SourceConnectorProtocol | None:
        """Get a source connector by name, or None if not found."""
        return self._sources.get(name)

    def has_strategy(self, name: str) -> bool:
        """Check if a strategy is registered."""
        return name in self._strategies

    def strategy_names(self) -> list[str]:
        """List all registered strategy names."""
        return list(self._strategies.keys())

    def pipeline_type_names(self) -> list[str]:
        """List all registered pipeline type names."""
        return list(self._pipeline_types.keys())

    def dispatch_hooks(self, phase: HookPhase, context: HookContext) -> None:
        """Fire all hooks registered for a phase. Errors are logged, not raised."""
        for hook in self.get_hooks(phase):
            try:
                hook(context)
            except Exception:
                logger.warning(
                    "Hook failed at phase '%s'",
                    phase,
                    exc_info=True,
                )
