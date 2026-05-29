"""Plugin protocols — defines the extension point contracts for the runner.

Six extension groups:
  rat.strategies      Custom merge strategies
  rat.pipeline_types  New pipeline languages (R, Scala, etc.)
  rat.jinja_helpers   Custom Jinja template functions
  rat.hooks           Pre/post phase callbacks
  rat.sources         External data source connectors
  rat.warehouses      Pluggable storage substrate / catalog (ADR-024)
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Literal, Protocol, runtime_checkable

if TYPE_CHECKING:
    import duckdb
    import pyarrow as pa

    from rat_runner.config import NessieConfig, S3Config
    from rat_runner.models import PipelineConfig, PipelineLogger

logger = logging.getLogger(__name__)


HookPhase = Literal[
    "pre_execute",
    "post_execute",
    "pre_write",
    "post_write",
    "pre_quality",
    "post_quality",
]


@dataclass
class HookContext:
    """Context passed to hook callbacks at phase boundaries."""

    namespace: str
    layer: str
    name: str
    run_id: str
    config: PipelineConfig | None
    logger: PipelineLogger
    branch: str = ""
    extra: dict[str, object] = field(default_factory=dict)


@runtime_checkable
class MergeStrategyProtocol(Protocol):
    """Protocol for custom merge strategies.

    Implementations receive the result Arrow table and write it to Iceberg
    using whatever merge logic they define.
    """

    @property
    def name(self) -> str:
        """Unique strategy name (e.g. 'full_refresh', 'incremental')."""
        ...

    def execute(
        self,
        data: pa.Table,
        table_name: str,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        location: str,
        config: PipelineConfig | None,
        branch: str = "main",
        conn: duckdb.DuckDBPyConnection | None = None,
    ) -> int:
        """Execute the merge strategy and return the number of rows written."""
        ...


@runtime_checkable
class PipelineTypeProtocol(Protocol):
    """Protocol for custom pipeline types (languages).

    Implementations receive the raw source code and return a PyArrow table.
    """

    @property
    def name(self) -> str:
        """Pipeline type identifier (e.g. 'sql', 'python', 'r')."""
        ...

    @property
    def file_extension(self) -> str:
        """File extension without dot (e.g. 'sql', 'py', 'r')."""
        ...

    def execute(
        self,
        source: str,
        namespace: str,
        layer: str,
        pipeline_name: str,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        config: PipelineConfig | None,
    ) -> pa.Table:
        """Execute the pipeline source and return a result table."""
        ...


@runtime_checkable
class JinjaHelperProtocol(Protocol):
    """Protocol for custom Jinja template functions.

    Each helper exposes a name and a callable that can be used in SQL templates.
    """

    @property
    def name(self) -> str:
        """Function name as it appears in templates (e.g. 'my_func')."""
        ...

    def __call__(self, *args: object, **kwargs: object) -> object:
        """The template function itself."""
        ...


@runtime_checkable
class HookProtocol(Protocol):
    """Protocol for pre/post phase hooks.

    Hooks are called at phase boundaries (pre_execute, post_execute, etc.)
    with a HookContext providing run metadata.
    """

    @property
    def phase(self) -> HookPhase:
        """Which phase boundary this hook fires at."""
        ...

    def __call__(self, context: HookContext) -> None:
        """Execute the hook."""
        ...


@runtime_checkable
class SourceConnectorProtocol(Protocol):
    """Protocol for external data source connectors.

    Source connectors fetch data from external systems and return Arrow tables.
    """

    @property
    def name(self) -> str:
        """Source type identifier (e.g. 'postgres', 'mysql', 'api')."""
        ...

    def fetch(
        self,
        config: dict[str, object],
        s3_config: S3Config,
    ) -> pa.Table:
        """Fetch data from the external source and return as Arrow table."""
        ...


# ── Warehouse (rat.warehouses) ───────────────────────────────────────────────
# The storage-substrate seam (ADR-024). Today's Iceberg+Nessie integration is
# being refactored into the reference implementation behind this Protocol so
# alternative catalogs/formats (Unity, Glue, Polaris, DuckLake, Delta) arrive as
# plugins rather than core edits.

WarehouseCapability = Literal[
    "branching",  # ephemeral write branches + merge (Nessie-style)
    "time_travel",  # read an older snapshot/version of a table
    "row_diff",  # row-level diff between two refs/versions
    "scd2_native",  # native SCD2 support in the storage layer
    "partition_evolution",  # repartition without rewriting history
]


@dataclass(frozen=True)
class TableRef:
    """Addresses a table within a warehouse: ``namespace.layer.name``."""

    namespace: str
    layer: str
    name: str


@runtime_checkable
class WarehouseProtocol(Protocol):
    """Protocol for a pluggable storage substrate / catalog (ADR-024).

    A warehouse owns table discovery, read attachment for runners, and writes
    (routing to a compatible merge-strategy implementation). The universal
    surface below is always present.

    Optional, capability-gated operations (history / branch / row-diff) are NOT
    part of this base Protocol: a caller first checks ``capabilities`` and then
    invokes them through the capability-specific accessor added in a later
    slice. This keeps warehouses that lack a feature from being forced to stub
    it.

    NOTE (ADR-024, slice 1): this is the contract only. The iceberg-nessie
    reference implementation and the consumer migration (ratq, diff,
    docs-assistant, pg-sync) land in later slices — defining this Protocol
    changes no runtime behaviour.
    """

    @property
    def name(self) -> str:
        """Warehouse identifier (e.g. 'iceberg-nessie', 'unity', 'ducklake')."""
        ...

    @property
    def capabilities(self) -> frozenset[WarehouseCapability]:
        """Optional features this warehouse supports. Callers gate on this set."""
        ...

    def list_namespaces(self) -> list[str]:
        """List namespaces known to the warehouse."""
        ...

    def list_tables(self, namespace: str) -> list[str]:
        """List table names within a namespace."""
        ...

    def get_schema(self, ref: TableRef) -> pa.Schema:
        """Return the Arrow schema of a table."""
        ...

    def attach_for_runner(
        self,
        runner_type: str,
        conn: duckdb.DuckDBPyConnection,
        *,
        branch: str = "main",
    ) -> None:
        """Make the warehouse's tables readable by a runner.

        For a DuckDB-backed runner this performs the catalog ATTACH / view
        wiring; the runner itself stays warehouse-agnostic.
        """
        ...

    def write(
        self,
        ref: TableRef,
        data: pa.Table,
        strategy: str,
        opts: PipelineConfig | None,
        *,
        branch: str = "main",
    ) -> int:
        """Write ``data`` to ``ref`` using the named strategy; return rows written.

        The warehouse resolves the (warehouse, strategy) pair to a compatible
        strategy implementation (ADR-024 §3 — strategy *name* is universal, the
        *implementation* declares its supported warehouses).
        """
        ...
