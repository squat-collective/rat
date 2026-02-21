"""Plugin protocols — defines the extension point contracts for the runner.

Five extension groups:
  rat.strategies      Custom merge strategies
  rat.pipeline_types  New pipeline languages (R, Scala, etc.)
  rat.jinja_helpers   Custom Jinja template functions
  rat.hooks           Pre/post phase callbacks
  rat.sources         External data source connectors
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
