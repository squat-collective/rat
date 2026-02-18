"""Pipeline executor — orchestrates the full pipeline lifecycle.

Execution flow:
  Phase 0: Create ephemeral Nessie branch
  Phase 1: Detect pipeline type (.py first, then .sql), read config.yaml
  Phase 2: Build result table (SQL or Python path)
  Phase 3: Write to Iceberg (full_refresh → overwrite, incremental → merge)
  Phase 4: Quality tests on ephemeral branch
  Phase 5: Branch resolution (merge or delete based on quality results)
"""

from __future__ import annotations

import logging
import time
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Literal

if TYPE_CHECKING:
    import pyarrow as pa

from rat_runner.config import (
    DuckDBConfig,
    NessieConfig,
    S3Config,
    list_s3_keys,
    merge_configs,
    move_s3_keys,
    parse_pipeline_config,
    read_s3_text,
    read_s3_text_version,
)
from rat_runner.engine import DuckDBEngine
from rat_runner.iceberg import (
    append_iceberg,
    delete_insert_iceberg,
    merge_iceberg,
    read_watermark,
    scd2_iceberg,
    snapshot_iceberg,
    write_iceberg,
)
from rat_runner.log import RunLogger
from rat_runner.maintenance import run_maintenance
from rat_runner.models import MergeStrategy, PipelineConfig, QualityTestResult, RunState, RunStatus
from rat_runner.nessie import create_branch, delete_branch, merge_branch
from rat_runner.python_exec import execute_python_pipeline
from rat_runner.quality import has_error_failures, run_quality_tests
from rat_runner.templating import (
    compile_sql,
    extract_landing_zones,
    extract_metadata,
    validate_landing_zones,
)

logger = logging.getLogger(__name__)


class CancelledError(Exception):
    """Raised when a run is cancelled between phases."""


def _check_cancelled(run: RunState) -> None:
    if run.cancel_event.is_set():
        raise CancelledError("Run cancelled")


@dataclass
class _PipelineContext:
    """Carries mutable state between pipeline execution phases.

    This replaces the many local variables that were threaded through the old
    monolith function, making inter-phase data flow explicit.
    """

    run: RunState
    s3_config: S3Config
    nessie_config: NessieConfig
    log: RunLogger
    published_versions: dict[str, str] = field(default_factory=dict)

    # Set during Phase 0
    branch_name: str = ""
    branch_created: bool = False

    # Set during Phase 1
    pipeline_type: Literal["python", "sql"] = "sql"
    source: str = ""  # raw pipeline source code (.py or .sql)
    raw_py: str | None = None
    raw_sql: str | None = None
    config: PipelineConfig | None = None

    # Set during Phase 2
    engine: DuckDBEngine | None = None
    table_name: str = ""
    location: str = ""
    result: pa.Table | None = None
    row_count: int = 0


def _archive_landing_zones(
    source: str, namespace: str, run_id: str, s3_config: S3Config, log: RunLogger
) -> list[str]:
    """Move landing zone files to _processed/{run_id}/ subfolder. Best-effort.

    Returns list of archived zone identifiers as "{namespace}/{zone}".
    """
    zones = extract_landing_zones(source)
    archived: list[str] = []
    for zone in zones:
        prefix = f"{namespace}/landing/{zone}/"
        dest_prefix = f"{namespace}/landing/{zone}/_processed/{run_id}/"
        try:
            keys = list_s3_keys(s3_config, prefix)
            # Filter out already-processed files
            keys = [k for k in keys if "/_processed/" not in k]
            if keys:
                move_s3_keys(s3_config, keys, prefix, dest_prefix)
                log.info(f"Archived {len(keys)} file(s) from landing zone '{zone}'")
                archived.append(f"{namespace}/{zone}")
            else:
                log.info(f"No files to archive in landing zone '{zone}'")
        except Exception as e:
            log.warn(f"Failed to archive landing zone '{zone}': {e}")
    return archived


def _format_quality_error(results: list[QualityTestResult]) -> str:
    """Build a descriptive error string from failed quality test results."""
    failed = [r for r in results if r.severity == "error" and r.status in ("fail", "error")]
    lines = ["Quality tests failed:"]
    for r in failed:
        label = f"  {r.test_name}"
        if r.description:
            label += f" ({r.description})"
        if r.status == "error":
            lines.append(f"{label}: errored — {r.message}")
        else:
            lines.append(f"{label}: {r.row_count} violation(s)")
        if r.sample_rows:
            for row_line in r.sample_rows.splitlines():
                lines.append(f"    {row_line}")
    return "\n".join(lines)


def _read_versioned(
    s3_config: S3Config, key: str, published_versions: dict[str, str]
) -> str | None:
    """Read from pinned version if available, else HEAD."""
    vid = published_versions.get(key)
    if vid:
        return read_s3_text_version(s3_config, key, vid)
    return read_s3_text(s3_config, key)


# ── Phase 0: Create ephemeral Nessie branch ──────────────────────────


def _phase0_create_branch(ctx: _PipelineContext) -> None:
    """Create an ephemeral Nessie branch for isolation.

    Falls back to writing directly to main if branch creation fails.
    """
    _check_cancelled(ctx.run)
    ctx.branch_name = f"run-{ctx.run.run_id}"
    ctx.log.info(f"Creating ephemeral branch '{ctx.branch_name}'")
    try:
        create_branch(ctx.nessie_config, ctx.branch_name, from_branch="main")
        ctx.branch_created = True
        ctx.run.branch = ctx.branch_name
        ctx.log.info(f"Branch '{ctx.branch_name}' created")
    except Exception as e:
        ctx.log.warn(f"Branch creation failed ({e}), writing to main")
        ctx.branch_name = "main"
        ctx.run.branch = "main"


# ── Phase 1: Detect pipeline type + read config ─────────────────────


def _phase1_detect_and_load(ctx: _PipelineContext) -> None:
    """Detect pipeline type (.py or .sql) and load merged config.

    Reads the pipeline source from S3, detects type by file extension priority
    (.py first, then .sql), merges config.yaml with source annotations, and
    validates landing zones.
    """
    _check_cancelled(ctx.run)
    ns, layer, name = ctx.run.namespace, ctx.run.layer, ctx.run.pipeline_name
    base_prefix = f"{ns}/pipelines/{layer}/{name}"

    py_key = f"{base_prefix}/pipeline.py"
    sql_key = f"{base_prefix}/pipeline.sql"
    config_key = f"{base_prefix}/config.yaml"

    pv = ctx.published_versions

    ctx.raw_py = _read_versioned(ctx.s3_config, py_key, pv)
    ctx.raw_sql = _read_versioned(ctx.s3_config, sql_key, pv) if ctx.raw_py is None else None

    if ctx.raw_py is None and ctx.raw_sql is None:
        raise FileNotFoundError(f"Pipeline not found: neither {py_key} nor {sql_key} exist")

    ctx.pipeline_type = "python" if ctx.raw_py is not None else "sql"
    ctx.log.info(f"Detected {ctx.pipeline_type} pipeline")

    # Load config: merge config.yaml base with annotation overrides
    source = ctx.raw_py if ctx.raw_py is not None else ctx.raw_sql
    assert source is not None
    ctx.source = source

    annotation_meta = extract_metadata(source)
    config_yaml = _read_versioned(ctx.s3_config, config_key, pv)
    base_config = parse_pipeline_config(config_yaml) if config_yaml else None
    if annotation_meta or base_config:
        ctx.config = merge_configs(base_config, annotation_meta)
        if annotation_meta and base_config:
            ctx.log.info(f"Merged config.yaml + annotations: {list(annotation_meta.keys())}")
        elif annotation_meta:
            ctx.log.info(f"Loaded config from source annotations: {list(annotation_meta.keys())}")
        else:
            ctx.log.info("Loaded pipeline config from config.yaml")

    lz_warnings = validate_landing_zones(source, ns, ctx.s3_config)
    for warn in lz_warnings:
        ctx.log.warn(warn)


# ── Phase 2: Build result table ──────────────────────────────────────


def _phase2_build_result(ctx: _PipelineContext) -> None:
    """Execute the pipeline (SQL or Python) and produce the result Arrow table."""
    _check_cancelled(ctx.run)
    ns, layer, name = ctx.run.namespace, ctx.run.layer, ctx.run.pipeline_name

    ctx.engine = DuckDBEngine(ctx.s3_config, DuckDBConfig.from_env())
    ctx.table_name = f"{ns}.{layer}.{name}"
    ctx.location = f"s3://{ctx.s3_config.bucket}/{ns}/{layer}/{name}/"

    if ctx.pipeline_type == "python":
        ctx.log.info("Executing Python pipeline")
        ctx.result = execute_python_pipeline(
            ctx.raw_py,  # type: ignore[arg-type]
            ctx.engine,
            ns,
            layer,
            name,
            ctx.s3_config,
            ctx.nessie_config,
            ctx.config,
        )
    else:
        ctx.result = _execute_sql_path(ctx)

    ctx.row_count = len(ctx.result)
    ctx.log.info(f"Query returned {ctx.row_count} rows")


def _execute_sql_path(ctx: _PipelineContext) -> pa.Table:
    """Handle the SQL pipeline path: watermark read, compile, execute."""
    watermark_value: str | None = None
    if (
        ctx.config is not None
        and ctx.config.merge_strategy in (MergeStrategy.INCREMENTAL, MergeStrategy.DELETE_INSERT)
        and ctx.config.watermark_column
    ):
        ctx.log.info(f"Reading watermark for column '{ctx.config.watermark_column}'")
        watermark_value = read_watermark(
            ctx.table_name,
            ctx.config.watermark_column,
            ctx.s3_config,
            ctx.nessie_config,
            branch="main",
            conn=ctx.engine.conn if ctx.engine else None,
        )
        if watermark_value:
            ctx.log.info(f"Watermark value: {watermark_value}")
        else:
            ctx.log.info("No watermark found (first run or empty table)")

    ns, layer, name = ctx.run.namespace, ctx.run.layer, ctx.run.pipeline_name

    ctx.log.info("Compiling SQL template")
    compiled_sql = compile_sql(
        ctx.raw_sql,  # type: ignore[arg-type]
        ns,
        layer,
        name,
        ctx.s3_config,
        ctx.nessie_config,
        config=ctx.config,
        watermark_value=watermark_value,
    )
    ctx.log.debug(f"Compiled SQL:\n{compiled_sql}")

    ctx.log.info("Executing SQL via DuckDB")
    assert ctx.engine is not None
    return ctx.engine.query_arrow(compiled_sql)


# ── Phase 3: Write to Iceberg ────────────────────────────────────────


def _phase3_write_iceberg(ctx: _PipelineContext) -> None:
    """Write the result table to Iceberg using the configured merge strategy.

    Dispatches to the appropriate write function based on merge strategy.
    Falls back to full_refresh when required config (unique_key, partition_column)
    is missing.
    """
    _check_cancelled(ctx.run)
    assert ctx.result is not None

    if ctx.row_count == 0:
        ctx.log.info("Zero rows — skipping Iceberg write")
        ctx.run.rows_written = 0
        return

    strategy = ctx.config.merge_strategy if ctx.config else MergeStrategy.FULL_REFRESH

    _engine_conn = ctx.engine.conn if ctx.engine else None
    _partition_by = ctx.config.partition_by if ctx.config and ctx.config.partition_by else None

    if strategy == MergeStrategy.INCREMENTAL and ctx.config and ctx.config.unique_key:
        ctx.log.info(f"Merging {ctx.row_count} rows into Iceberg table {ctx.table_name}")
        merged_rows = merge_iceberg(
            ctx.result,
            ctx.table_name,
            ctx.config.unique_key,
            ctx.s3_config,
            ctx.nessie_config,
            ctx.location,
            branch=ctx.branch_name,
            conn=_engine_conn,
            partition_by=_partition_by,
        )
        ctx.run.rows_written = merged_rows
        ctx.log.info(f"Merge complete ({merged_rows} total rows)")

    elif strategy == MergeStrategy.APPEND_ONLY:
        ctx.log.info(f"Appending {ctx.row_count} rows to Iceberg table {ctx.table_name}")
        appended = append_iceberg(
            ctx.result,
            ctx.table_name,
            ctx.s3_config,
            ctx.nessie_config,
            ctx.location,
            branch=ctx.branch_name,
            partition_by=_partition_by,
        )
        ctx.run.rows_written = appended
        ctx.log.info(f"Append complete ({appended} rows)")

    elif strategy == MergeStrategy.DELETE_INSERT and ctx.config and ctx.config.unique_key:
        ctx.log.info(f"Delete-insert {ctx.row_count} rows into Iceberg table {ctx.table_name}")
        total = delete_insert_iceberg(
            ctx.result,
            ctx.table_name,
            ctx.config.unique_key,
            ctx.s3_config,
            ctx.nessie_config,
            ctx.location,
            branch=ctx.branch_name,
            conn=_engine_conn,
            partition_by=_partition_by,
        )
        ctx.run.rows_written = total
        ctx.log.info(f"Delete-insert complete ({total} total rows)")

    elif strategy == MergeStrategy.SCD2 and ctx.config and ctx.config.unique_key:
        ctx.log.info(f"SCD2 merge {ctx.row_count} rows into Iceberg table {ctx.table_name}")
        total = scd2_iceberg(
            ctx.result,
            ctx.table_name,
            ctx.config.unique_key,
            ctx.s3_config,
            ctx.nessie_config,
            ctx.location,
            valid_from_col=ctx.config.scd_valid_from,
            valid_to_col=ctx.config.scd_valid_to,
            branch=ctx.branch_name,
            conn=_engine_conn,
            partition_by=_partition_by,
        )
        ctx.run.rows_written = total
        ctx.log.info(f"SCD2 merge complete ({total} total rows)")

    elif strategy == MergeStrategy.SNAPSHOT and ctx.config and ctx.config.partition_column:
        ctx.log.info(f"Snapshot {ctx.row_count} rows into Iceberg table {ctx.table_name}")
        total = snapshot_iceberg(
            ctx.result,
            ctx.table_name,
            ctx.config.partition_column,
            ctx.s3_config,
            ctx.nessie_config,
            ctx.location,
            branch=ctx.branch_name,
            conn=_engine_conn,
            partition_by=_partition_by,
        )
        ctx.run.rows_written = total
        ctx.log.info(f"Snapshot complete ({total} total rows)")

    else:
        _write_full_refresh_fallback(ctx, strategy)


def _write_full_refresh_fallback(ctx: _PipelineContext, strategy: MergeStrategy) -> None:
    """Write using full_refresh, warning if the intended strategy lacked required config."""
    assert ctx.result is not None

    if strategy in (MergeStrategy.INCREMENTAL, MergeStrategy.DELETE_INSERT, MergeStrategy.SCD2):
        if not (ctx.config and ctx.config.unique_key):
            ctx.log.warn(
                f"Strategy '{strategy}' requires unique_key — falling back to full_refresh"
            )
    elif strategy == MergeStrategy.SNAPSHOT and not (ctx.config and ctx.config.partition_column):
        ctx.log.warn("Strategy 'snapshot' requires partition_column — falling back to full_refresh")

    ctx.log.info(f"Writing {ctx.row_count} rows to Iceberg table {ctx.table_name}")
    partition_by = ctx.config.partition_by if ctx.config else None
    write_iceberg(
        ctx.result,
        ctx.table_name,
        ctx.s3_config,
        ctx.nessie_config,
        ctx.location,
        branch=ctx.branch_name,
        partition_by=partition_by or None,
    )
    ctx.run.rows_written = ctx.row_count
    ctx.log.info("Iceberg write complete")


# ── Phase 4: Quality tests ───────────────────────────────────────────


def _phase4_quality_tests(ctx: _PipelineContext) -> list[QualityTestResult]:
    """Run quality tests against the result data and return results."""
    _check_cancelled(ctx.run)
    assert ctx.engine is not None
    quality_results = run_quality_tests(
        ctx.run,
        ctx.engine,
        ctx.s3_config,
        ctx.nessie_config,
        ctx.log,
        published_versions=ctx.published_versions or None,
    )
    ctx.run.quality_results = quality_results
    return quality_results


# ── Phase 5: Branch resolution ───────────────────────────────────────


def _phase5_resolve_branch(ctx: _PipelineContext, quality_results: list[QualityTestResult]) -> None:
    """Merge or discard the ephemeral branch based on quality test results.

    On quality failure with branch isolation, the branch is deleted (no data
    reaches main). Without branch isolation, data is already on main and we
    can only report the failure.
    """
    if ctx.branch_created:
        _resolve_with_branch(ctx, quality_results)
    else:
        _resolve_without_branch(ctx, quality_results)


def _resolve_with_branch(ctx: _PipelineContext, quality_results: list[QualityTestResult]) -> None:
    """Handle branch resolution when an ephemeral branch was created."""
    if has_error_failures(quality_results):
        ctx.log.error("Quality tests failed — discarding branch (no data on main)")
        try:
            delete_branch(ctx.nessie_config, ctx.branch_name)
        except Exception as e:
            ctx.log.warn(f"Failed to delete branch: {e}")
        ctx.run.status = RunStatus.FAILED
        ctx.run.error = _format_quality_error(quality_results)
        return

    ctx.log.info(f"Merging branch '{ctx.branch_name}' to main")
    try:
        merge_branch(ctx.nessie_config, ctx.branch_name, target="main")
        ctx.log.info("Branch merged to main")
    except Exception as e:
        ctx.log.error(f"Branch merge failed: {e}")
        ctx.run.status = RunStatus.FAILED
        ctx.run.error = f"Branch merge failed: {e}"
        return

    _post_success(ctx)


def _resolve_without_branch(
    ctx: _PipelineContext, quality_results: list[QualityTestResult]
) -> None:
    """Handle resolution when no ephemeral branch was created (direct main writes)."""
    if has_error_failures(quality_results):
        ctx.run.status = RunStatus.FAILED
        ctx.run.error = _format_quality_error(quality_results)
        ctx.log.error("Quality tests failed (data already on main — no rollback available)")
        return

    _post_success(ctx)


# ── Post-success: archive + maintenance ──────────────────────────────


def _post_success(ctx: _PipelineContext) -> None:
    """Run post-success tasks: mark success, archive landing zones, run Iceberg maintenance."""
    ctx.run.status = RunStatus.SUCCESS
    ctx.log.info("Pipeline completed successfully")

    # Archive landing zone files (opt-in)
    if ctx.config is not None and ctx.config.archive_landing_zones:
        ns = ctx.run.namespace
        ctx.run.archived_zones = _archive_landing_zones(
            ctx.source,
            ns,
            ctx.run.run_id,
            ctx.s3_config,
            ctx.log,
        )

    # Iceberg maintenance (best-effort)
    if ctx.row_count > 0:
        try:
            run_maintenance(ctx.table_name, ctx.s3_config, ctx.nessie_config, log=ctx.log)
        except Exception as e:
            ctx.log.warn(f"Iceberg maintenance failed (non-fatal): {e}")


# ── Public entry point ───────────────────────────────────────────────


def execute_pipeline(
    run: RunState,
    s3_config: S3Config,
    nessie_config: NessieConfig,
    published_versions: dict[str, str] | None = None,
) -> None:
    """Execute a pipeline run. Intended to run in a worker thread.

    Updates RunState in-place with status, rows_written, duration_ms, and error.

    Pipeline paths (S3):
        SQL:    {namespace}/pipelines/{layer}/{name}/pipeline.sql
        Python: {namespace}/pipelines/{layer}/{name}/pipeline.py
        Config: {namespace}/pipelines/{layer}/{name}/config.yaml
    Iceberg table: {namespace}.{layer}.{name}
    Iceberg loc:   s3://{bucket}/{namespace}/{layer}/{name}/
    """
    log = RunLogger(run)
    start = time.monotonic()
    run.status = RunStatus.RUNNING

    # Per-run env overrides: apply to S3Config instead of os.environ
    # (os.environ is process-global and thread-unsafe for concurrent runs)
    if run.env:
        s3_config = s3_config.with_overrides(run.env)

    ctx = _PipelineContext(
        run=run,
        s3_config=s3_config,
        nessie_config=nessie_config,
        log=log,
        published_versions=published_versions or {},
    )

    try:
        _phase0_create_branch(ctx)
        _phase1_detect_and_load(ctx)
        _phase2_build_result(ctx)
        _phase3_write_iceberg(ctx)
        quality_results = _phase4_quality_tests(ctx)
        _phase5_resolve_branch(ctx, quality_results)

    except CancelledError:
        run.status = RunStatus.CANCELLED
        run.error = "Run cancelled by user"
        log.warn("Pipeline cancelled")

    except Exception as e:
        run.status = RunStatus.FAILED
        run.error = str(e)
        log.error(f"Pipeline failed: {e}")

    finally:
        # Cleanup: delete ephemeral branch if still exists
        if ctx.branch_created and ctx.branch_name != "main":
            try:
                delete_branch(nessie_config, ctx.branch_name)
            except Exception:
                logger.warning(
                    "Failed to delete ephemeral branch '%s'",
                    ctx.branch_name,
                    exc_info=True,
                )

        if ctx.engine is not None:
            ctx.engine.close()
        elapsed_ms = int((time.monotonic() - start) * 1000)
        run.duration_ms = elapsed_ms
        log.info(f"Duration: {elapsed_ms}ms")
