"""PRQL pipeline type — write RAT transforms in PRQL instead of SQL.

PRQL (https://prql-lang.org) is a modern, composable query language that
compiles to SQL. This plugin registers the ``prql`` pipeline type: any
``pipeline.prql`` file is compiled to SQL and executed on DuckDB, exactly
like a core ``pipeline.sql`` file.

Example ``pipeline.prql`` (a bronze pipeline reading a landing zone):

    from s"read_csv_auto('s3://rat/default/landing/orders/*.csv')"
    filter amount > 100
    derive {amount_eur = amount * 0.92}

PRQL s-strings (``s"..."``) pass raw SQL through — that is how a pipeline
reads landing-zone files or calls DuckDB table functions.

Demonstrates the ``rat.pipeline_types`` extension point.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import prqlc

from rat_runner.config import DuckDBConfig
from rat_runner.engine import DuckDBEngine

if TYPE_CHECKING:
    import pyarrow as pa

    from rat_runner.config import NessieConfig, S3Config
    from rat_runner.models import PipelineConfig


class PrqlPipelineType:
    """Pipeline type that compiles PRQL source to SQL and runs it on DuckDB."""

    @property
    def name(self) -> str:
        return "prql"

    @property
    def file_extension(self) -> str:
        return "prql"

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
        """Compile the PRQL source to SQL and execute it, returning an Arrow table."""
        sql = compile_prql(source)

        # Run the compiled SQL on a DuckDB connection with S3/Iceberg configured
        # — the same engine the core SQL pipeline type uses.
        engine = DuckDBEngine(s3_config, DuckDBConfig.from_env())
        try:
            return engine.query_arrow(sql)
        finally:
            engine.close()


def compile_prql(source: str) -> str:
    """Compile a PRQL string to SQL. Raises on invalid PRQL."""
    return prqlc.compile(source)
