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
# The storage-substrate seam (ADR-024). The cross-language wire contract is
# warehouse/v1 (ConnectRPC, see proto/warehouse/v1). These Protocols are the
# Python AUTHOR SDK: a warehouse plugin implements WarehouseProtocol (+ any
# optional capability Protocols it supports) and an adapter (a later slice)
# serves it as the gRPC WarehouseService. Today's Iceberg+Nessie integration
# becomes the reference impl behind it.

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


@dataclass(frozen=True)
class AttachDescriptor:
    """What a runner needs to READ a warehouse, without baking in an engine.

    Backend-agnostic (ADR-024 §4): a DuckDB runner turns this into an ATTACH; a
    future engine consumes it however it needs. Mirrors warehouse/v1's
    ``AttachDescriptor`` message.
    """

    catalog_uri: str
    format: str  # e.g. "iceberg", "delta"
    storage: dict[str, str] = field(default_factory=dict)  # object-store creds/opts
    options: dict[str, str] = field(default_factory=dict)  # engine-specific extras


@dataclass(frozen=True)
class Snapshot:
    """One committed version of a table (TIME_TRAVEL)."""

    id: str
    committed_at: str  # ISO-8601
    message: str
    rows: int


@dataclass(frozen=True)
class RowDiff:
    """Row-level diff between two refs (ROW_DIFF)."""

    added: pa.Table
    removed: pa.Table


@runtime_checkable
class WarehouseProtocol(Protocol):
    """Required surface every warehouse implements (ADR-024).

    Discovery, read-attach, and write. Optional features (branching, time
    travel, row diff) are SEPARATE Protocols (below) that a warehouse opts into;
    a caller checks ``capabilities`` and then ``isinstance(wh, BranchingWarehouse)``
    etc. before using them, so a warehouse that lacks a feature never stubs it.

    NOTE (ADR-024 slice 1): contract only — no runtime behaviour change. The
    iceberg-nessie reference impl + the warehouse/v1 serving adapter + the
    consumer migration land in later slices.
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

    def list_tables(self, namespace: str) -> list[TableRef]:
        """List tables within a namespace."""
        ...

    def get_schema(self, ref: TableRef, *, branch: str = "main") -> pa.Schema:
        """Return the Arrow schema of a table."""
        ...

    def attach(self, runner_type: str, *, branch: str = "main") -> AttachDescriptor:
        """Return an opaque descriptor a runner uses to read the warehouse.

        The runner stays warehouse-agnostic; the warehouse stays runner-agnostic
        (ADR-024 §4).
        """
        ...

    def write(
        self,
        ref: TableRef,
        data: pa.Table,
        strategy: str,
        options: dict[str, str] | None = None,
        *,
        branch: str = "main",
    ) -> int:
        """Write ``data`` to ``ref`` via the named strategy; return rows written.

        Strategy *name* is universal; the warehouse resolves it to a compatible
        *implementation* (ADR-024 §3/§5).
        """
        ...


@runtime_checkable
class BranchingWarehouse(Protocol):
    """Optional capability ``branching``: ephemeral write branches + merge."""

    def create_branch(self, name: str, *, from_ref: str = "main") -> str:
        """Create a branch off ``from_ref``; return its name."""
        ...

    def merge_branch(self, from_branch: str, *, into_branch: str = "main") -> None:
        """Merge ``from_branch`` into ``into_branch`` (raises on conflict)."""
        ...

    def delete_branch(self, name: str) -> bool:
        """Delete a branch; return False if it didn't exist (idempotent)."""
        ...

    def list_branches(self) -> list[str]:
        """List branch names."""
        ...


@runtime_checkable
class TimeTravelWarehouse(Protocol):
    """Optional capability ``time_travel``: read older snapshots / history."""

    def get_history(self, ref: TableRef, *, limit: int = 0) -> list[Snapshot]:
        """Return a table's commit history, newest first (0 => server default)."""
        ...


@runtime_checkable
class RowDiffWarehouse(Protocol):
    """Optional capability ``row_diff``: row-level diff between two refs."""

    def row_diff(self, ref: TableRef, from_ref: str, to_ref: str, *, limit: int = 0) -> RowDiff:
        """Return rows added/removed going from ``from_ref`` to ``to_ref``."""
        ...
