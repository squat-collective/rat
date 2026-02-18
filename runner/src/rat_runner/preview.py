"""Preview executor — ephemeral pipeline execution without writes."""

from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import TYPE_CHECKING

from rat_runner.config import NessieConfig, S3Config, read_s3_text

if TYPE_CHECKING:
    import pyarrow as pa
from rat_runner.engine import DuckDBEngine
from rat_runner.log import RunLogger
from rat_runner.models import LogRecord, PipelineConfig, RunState
from rat_runner.python_exec import execute_python_pipeline
from rat_runner.templating import (
    _resolve_landing_zone_preview,
    compile_sql,
    extract_metadata,
    metadata_to_config,
)

PREVIEW_TIMEOUT_SECONDS = 30
DEFAULT_PREVIEW_LIMIT = 100


@dataclass
class ColumnInfo:
    """Column metadata for preview results."""

    name: str
    type: str


@dataclass
class PhaseProfile:
    """Timing for a single execution phase."""

    name: str
    duration_ms: int
    metadata: dict[str, str] = field(default_factory=dict)


@dataclass
class PreviewResult:
    """Result of a pipeline preview execution."""

    arrow_table: pa.Table | None = None
    columns: list[ColumnInfo] = field(default_factory=list)
    total_row_count: int = 0
    phases: list[PhaseProfile] = field(default_factory=list)
    explain_output: str = ""
    memory_peak_bytes: int = 0
    logs: list[LogRecord] = field(default_factory=list)
    error: str = ""
    warnings: list[str] = field(default_factory=list)


def _time_ms(start: float) -> int:
    return int((time.monotonic() - start) * 1000)


def preview_pipeline(
    namespace: str,
    layer: str,
    pipeline_name: str,
    s3_config: S3Config,
    nessie_config: NessieConfig,
    preview_limit: int = DEFAULT_PREVIEW_LIMIT,
    code: str | None = None,
    pipeline_type: str | None = None,
) -> PreviewResult:
    """Execute a pipeline in preview mode — no writes, no branches, no quality tests.

    Returns sample rows, column info, timing profile, EXPLAIN ANALYZE output,
    memory stats, and execution logs.
    """
    result = PreviewResult()

    # Create a lightweight RunState for the logger (not persisted)
    run_state = RunState(
        run_id="preview",
        namespace=namespace,
        layer=layer,
        pipeline_name=pipeline_name,
        trigger="preview",
    )
    log = RunLogger(run_state)
    engine: DuckDBEngine | None = None

    try:
        engine = DuckDBEngine(s3_config)
        log.info(f"Starting preview for {namespace}/{layer}/{pipeline_name}")

        # --- Phase 1: Detect pipeline type + read config ---
        t0 = time.monotonic()
        layer_str = layer
        detected_type, source, config = _detect_pipeline(
            namespace,
            layer_str,
            pipeline_name,
            s3_config,
            log,
            code=code,
            pipeline_type_hint=pipeline_type,
        )
        pipeline_type = detected_type
        result.phases.append(
            PhaseProfile(
                name="detect",
                duration_ms=_time_ms(t0),
                metadata={"pipeline_type": pipeline_type},
            )
        )

        if pipeline_type == "sql":
            _preview_sql(
                source=source,
                namespace=namespace,
                layer=layer_str,
                pipeline_name=pipeline_name,
                s3_config=s3_config,
                nessie_config=nessie_config,
                config=config,
                engine=engine,
                log=log,
                result=result,
                preview_limit=preview_limit,
            )
        else:
            _preview_python(
                source=source,
                namespace=namespace,
                layer=layer_str,
                pipeline_name=pipeline_name,
                s3_config=s3_config,
                nessie_config=nessie_config,
                config=config,
                engine=engine,
                log=log,
                result=result,
                preview_limit=preview_limit,
            )

        # Collect memory stats
        try:
            mem_stats = engine.get_memory_stats()
            result.memory_peak_bytes = mem_stats.get("memory_usage", 0)
        except Exception:
            pass

        log.info("Preview completed successfully")

    except Exception as e:
        result.error = str(e)
        log.error(f"Preview failed: {e}")
    finally:
        if engine is not None:
            engine.close()
        # Collect logs from run state
        result.logs = list(run_state.logs)

    return result


def _detect_pipeline(
    namespace: str,
    layer: str,
    pipeline_name: str,
    s3_config: S3Config,
    log: RunLogger,
    code: str | None = None,
    pipeline_type_hint: str | None = None,
) -> tuple[str, str, PipelineConfig | None]:
    """Detect pipeline type and read source + config.

    If ``code`` is provided, uses it directly instead of reading from S3.
    ``pipeline_type_hint`` ("sql" or "python") disambiguates the type when
    inline code is given; defaults to "sql".
    """
    prefix = f"{namespace}/pipelines/{layer}/{pipeline_name}"

    # Inline code path — skip S3 reads for the source file
    if code is not None:
        ptype = pipeline_type_hint if pipeline_type_hint in ("sql", "python") else "sql"
        log.info(f"Using inline {ptype} code ({len(code)} chars)")
        config = _load_config(code, prefix, s3_config)
        return ptype, code, config

    # Try Python first, then SQL (same order as executor.py)
    py_source = read_s3_text(s3_config, f"{prefix}/pipeline.py")
    if py_source is not None:
        log.info("Detected Python pipeline")
        config = _load_config(py_source, prefix, s3_config)
        return "python", py_source, config

    sql_source = read_s3_text(s3_config, f"{prefix}/pipeline.sql")
    if sql_source is not None:
        log.info("Detected SQL pipeline")
        config = _load_config(sql_source, prefix, s3_config)
        return "sql", sql_source, config

    raise FileNotFoundError(f"No pipeline.py or pipeline.sql found at {prefix}/")


def _load_config(
    source: str,
    prefix: str,
    s3_config: S3Config,
) -> PipelineConfig | None:
    """Load config from inline annotations or config.yaml."""
    metadata = extract_metadata(source)
    if metadata:
        return metadata_to_config(metadata)

    config_yaml = read_s3_text(s3_config, f"{prefix}/config.yaml")
    if config_yaml:
        from rat_runner.config import parse_pipeline_config

        return parse_pipeline_config(config_yaml)

    return None


def _extract_columns(table: pa.Table) -> list[ColumnInfo]:
    """Extract column names and types from a PyArrow table."""
    columns = []
    for schema_field in table.schema:
        columns.append(ColumnInfo(name=schema_field.name, type=str(schema_field.type)))
    return columns


def _preview_sql(
    source: str,
    namespace: str,
    layer: str,
    pipeline_name: str,
    s3_config: S3Config,
    nessie_config: NessieConfig,
    config: PipelineConfig | None,
    engine: DuckDBEngine,
    log: RunLogger,
    result: PreviewResult,
    preview_limit: int,
) -> None:
    """Run SQL pipeline preview — compile, execute with LIMIT, EXPLAIN ANALYZE, COUNT."""
    # Phase 2: Compile SQL
    t0 = time.monotonic()

    def preview_lz_fn(zone_name: str) -> str:
        return _resolve_landing_zone_preview(zone_name, namespace, s3_config, result.warnings)

    compiled_sql = compile_sql(
        raw_sql=source,
        namespace=namespace,
        layer=layer,
        pipeline_name=pipeline_name,
        s3_config=s3_config,
        nessie_config=nessie_config,
        config=config,
        landing_zone_fn=preview_lz_fn,
    )
    result.phases.append(PhaseProfile(name="compile", duration_ms=_time_ms(t0)))
    log.info("SQL compiled")

    # Phase 3: Execute with LIMIT
    t0 = time.monotonic()
    limited_sql = f"SELECT * FROM ({compiled_sql}) AS _preview LIMIT {preview_limit}"
    table = engine.query_arrow(limited_sql)
    result.phases.append(
        PhaseProfile(
            name="execute",
            duration_ms=_time_ms(t0),
            metadata={"limit": str(preview_limit)},
        )
    )
    result.arrow_table = table
    result.columns = _extract_columns(table)
    log.info(f"Executed with LIMIT {preview_limit}: {table.num_rows} rows")

    # Phase 4: EXPLAIN ANALYZE
    # Use the limited SQL to avoid re-executing the full query.  The query
    # plan for the LIMIT-wrapped version is representative enough for
    # preview purposes and avoids a second full-data scan.
    t0 = time.monotonic()
    try:
        explain_text = engine.explain_analyze(limited_sql)
        result.explain_output = explain_text
    except Exception as e:
        result.warnings.append(f"EXPLAIN ANALYZE failed: {e}")
        log.warn(f"EXPLAIN ANALYZE failed: {e}")
    result.phases.append(PhaseProfile(name="explain", duration_ms=_time_ms(t0)))

    # Phase 5: COUNT(*)
    # Skip the extra full-query execution when the LIMIT query already returned
    # fewer rows than requested — that means we have the exact total.
    t0 = time.monotonic()
    if table.num_rows < preview_limit:
        result.total_row_count = table.num_rows
    else:
        try:
            count_result = engine.conn.execute(
                f"SELECT COUNT(*) FROM ({compiled_sql}) AS _count"
            ).fetchone()
            result.total_row_count = count_result[0] if count_result else 0
        except Exception as e:
            result.warnings.append(f"COUNT(*) failed: {e}")
            result.total_row_count = table.num_rows
            log.warn(f"COUNT(*) failed: {e}")
    result.phases.append(PhaseProfile(name="count", duration_ms=_time_ms(t0)))
    log.info(f"Total row count: {result.total_row_count}")


def _preview_python(
    source: str,
    namespace: str,
    layer: str,
    pipeline_name: str,
    s3_config: S3Config,
    nessie_config: NessieConfig,
    config: PipelineConfig | None,
    engine: DuckDBEngine,
    log: RunLogger,
    result: PreviewResult,
    preview_limit: int,
) -> None:
    """Run Python pipeline preview — execute with logger, slice result."""
    # Phase 2: Compile (no-op for Python)
    result.phases.append(
        PhaseProfile(
            name="compile",
            duration_ms=0,
            metadata={"skipped": "python"},
        )
    )

    # Phase 3: Execute
    t0 = time.monotonic()

    def preview_lz_fn(zone_name: str) -> str:
        return _resolve_landing_zone_preview(zone_name, namespace, s3_config, result.warnings)

    table = execute_python_pipeline(
        source=source,
        engine=engine,
        namespace=namespace,
        layer=layer,
        name=pipeline_name,
        s3_config=s3_config,
        nessie_config=nessie_config,
        config=config,
        logger=log,
        landing_zone_fn=preview_lz_fn,
    )
    result.phases.append(
        PhaseProfile(
            name="execute",
            duration_ms=_time_ms(t0),
            metadata={"limit": str(preview_limit)},
        )
    )

    # Slice to limit
    total = table.num_rows
    if total > preview_limit:
        table = table.slice(0, preview_limit)

    result.arrow_table = table
    result.columns = _extract_columns(table)
    result.total_row_count = total
    log.info(f"Executed Python pipeline: {table.num_rows} rows (total: {total})")

    # Phase 4: EXPLAIN (N/A for Python)
    result.phases.append(
        PhaseProfile(
            name="explain",
            duration_ms=0,
            metadata={"skipped": "python"},
        )
    )

    # Phase 5: COUNT (already have total)
    result.phases.append(PhaseProfile(name="count", duration_ms=0))
