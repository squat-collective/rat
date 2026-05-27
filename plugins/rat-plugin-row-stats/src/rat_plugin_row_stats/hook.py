"""Post-write hook that logs row counts and pipeline metadata.

Fires at the post_write phase and logs a summary of what was written.
All operations are wrapped in try/except — hooks must never crash pipelines.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from rat_runner.plugin_protocols import HookContext, HookPhase


class RowStatsHook:
    """Logs row counts and config details after each Iceberg write.

    Example log output:
        [row-stats] ns.silver.orders: wrote 1500 rows (strategy: incremental)
        [row-stats] unique_key: (id, date)
    """

    @property
    def phase(self) -> HookPhase:
        return "post_write"

    def __call__(self, context: HookContext) -> None:
        try:
            table_ref = f"{context.namespace}.{context.layer}.{context.name}"
            strategy = "unknown"
            if context.config is not None:
                strategy = str(context.config.merge_strategy)

            rows = context.extra.get("rows_written", "?")
            context.logger.info(
                f"[row-stats] {table_ref}: wrote {rows} rows (strategy: {strategy})"
            )

            if context.config is not None and context.config.unique_key:
                key_str = ", ".join(context.config.unique_key)
                context.logger.info(f"[row-stats] unique_key: ({key_str})")
        except Exception:
            # Hooks must never crash pipelines — swallow all errors silently.
            pass
