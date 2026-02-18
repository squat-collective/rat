# ADR-014: Merge Strategies

## Status: Accepted

## Context

RAT v2.1 shipped with two write modes: **full refresh** (overwrite) and **incremental**
(ANTI JOIN dedup merge). These cover the common cases, but real-world data pipelines need more
nuanced strategies:

- **Event/log tables** should never overwrite — append-only semantics
- **Staging loads** need delete-and-replace without dedup overhead
- **Slowly changing dimensions** need SCD Type 2 history tracking
- **Partitioned fact tables** should only replace touched partitions, not the whole table

Additionally, config management had a limitation: if ANY `@annotation` existed in the pipeline
source, the `config.yaml` file was entirely ignored. This forced users to choose between
config.yaml management (via portal UI) and inline annotations — they couldn't use both.

## Decision

### 6 Merge Strategies

The runner supports 6 merge strategies, configured via `merge_strategy` in `config.yaml` or
the `@merge_strategy` source annotation:

| Strategy | Behavior | Required Fields |
|----------|----------|-----------------|
| `full_refresh` | Overwrite entire table each run (default) | — |
| `incremental` | ANTI JOIN merge on unique key with dedup (ROW_NUMBER QUALIFY) | `unique_key`, `watermark_column` (optional) |
| `append_only` | Always append, never overwrite existing rows | — |
| `delete_insert` | Delete rows matching unique key, insert all new (no dedup) | `unique_key` |
| `scd2` | SCD Type 2 — track history with valid_from/valid_to columns | `unique_key`, `scd_valid_from` (default: `valid_from`), `scd_valid_to` (default: `valid_to`) |
| `snapshot` | Replace only partitions present in new data | `partition_column` |

### Config Merge: Annotations Overlay config.yaml

The previous either/or config logic is replaced with a **merge** approach:

1. `config.yaml` (from S3) is loaded as the **base** config
2. Source annotations (`@key: value`) are parsed as **overrides**
3. Per-field merge: annotation values win over config.yaml values
4. Missing fields in annotations fall through to config.yaml values

This lets the portal write `config.yaml` while users can still fine-tune with annotations.
The portal UI becomes the primary config surface, with annotations as power-user overrides.

```python
# Merge logic (config.py)
def merge_configs(base: PipelineConfig | None, annotations: dict) -> PipelineConfig:
    b = base or PipelineConfig()  # defaults
    return PipelineConfig(
        merge_strategy=annotations.get("merge_strategy", b.merge_strategy),
        unique_key=parse_or_fallback(annotations, "unique_key", b.unique_key),
        # ... per-field merge for all config fields
    )
```

### 4 New Iceberg Write Functions

Each strategy maps to a dedicated write function in `iceberg.py`:

**`append_iceberg(data, table_name, ...)`**
- Pure PyIceberg `table.append(data)` if table exists
- Falls back to `write_iceberg()` (overwrite) on first run
- Simplest strategy — no DuckDB SQL needed

**`delete_insert_iceberg(data, table_name, unique_key, ...)`**
- Same ANTI JOIN pattern as `merge_iceberg()` but **without dedup** (no ROW_NUMBER QUALIFY)
- SQL: `SELECT e.* FROM existing WHERE NOT EXISTS (key match) UNION ALL SELECT * FROM new_data`
- Faster than incremental when you know incoming data has no duplicates

**`scd2_iceberg(data, table_name, unique_key, valid_from_col, valid_to_col, ...)`**
- Most complex strategy. DuckDB SQL in 4 steps:
  1. Open records matching new keys → close them (set `valid_to = CURRENT_TIMESTAMP`)
  2. Open records NOT matching new keys → keep unchanged
  3. Already-closed historical records → keep unchanged
  4. New records → add `valid_from = CURRENT_TIMESTAMP`, `valid_to = NULL`
- First run: adds SCD columns (`valid_from`, `valid_to`) to the data before writing
- Falls back to `write_iceberg()` on first run

**`snapshot_iceberg(data, table_name, partition_column, ...)`**
- Partition-aware overwrite via DuckDB SQL:
  `SELECT * FROM existing WHERE partition_col NOT IN (new partitions) UNION ALL SELECT * FROM new_data`
- Only touches partitions present in the incoming data
- Falls back to `write_iceberg()` on first run

All functions share the pattern: get_catalog → ensure_namespace → try load_table →
DuckDB SQL merge → `table.overwrite(merged)`. `NoSuchTableError` triggers fallback to
`write_iceberg()` (create table on first run).

### Strategy Dispatch in Executor

The executor's phase 3 (write) uses a strategy switch:

```python
if strategy == "incremental" and config.unique_key:
    merge_iceberg(...)
elif strategy == "append_only":
    append_iceberg(...)
elif strategy == "delete_insert" and config.unique_key:
    delete_insert_iceberg(...)
elif strategy == "scd2" and config.unique_key:
    scd2_iceberg(..., config.scd_valid_from, config.scd_valid_to)
elif strategy == "snapshot" and config.partition_column:
    snapshot_iceberg(..., config.partition_column)
else:
    write_iceberg(...)  # full_refresh (default fallback)
```

If a strategy requires `unique_key` or `partition_column` but it's missing, the executor
logs a warning and falls back to `full_refresh`. This is a safety net, not an error.

### New Template Functions

4 new Jinja helpers available in SQL pipelines:

- `is_append_only()` → `True` when strategy is `append_only`
- `is_delete_insert()` → `True` when strategy is `delete_insert`
- `is_scd2()` → `True` when strategy is `scd2`
- `is_snapshot()` → `True` when strategy is `snapshot`

These complement the existing `is_incremental()` and allow strategy-specific SQL logic.

### New PipelineConfig Fields

```python
@dataclass(frozen=True)
class PipelineConfig:
    # ... existing fields ...
    partition_column: str = ""         # for snapshot strategy
    scd_valid_from: str = "valid_from" # for scd2 strategy
    scd_valid_to: str = "valid_to"     # for scd2 strategy
```

### Portal Merge Strategy Settings UI

A new `PipelineMergeStrategy` card component in the pipeline detail Settings tab lets users
configure merge strategy through the portal. It reads/writes `config.yaml` on S3 with a
read-merge-write pattern to preserve unmanaged fields.

### SDK Types

```typescript
export type MergeStrategy = "full_refresh" | "incremental" | "append_only"
  | "delete_insert" | "scd2" | "snapshot";

export interface PipelineConfig {
  merge_strategy?: MergeStrategy;
  unique_key?: string[];
  watermark_column?: string;
  partition_column?: string;
  scd_valid_from?: string;
  scd_valid_to?: string;
  materialized?: "table" | "view";
  archive_landing_zones?: boolean;
  description?: string;
}
```

## Consequences

### Positive

- **6 strategies** cover the vast majority of data pipeline write patterns
- **Config merge** (annotations overlay config.yaml) enables both portal UI management and
  power-user annotation overrides — no more either/or
- **Portal UI** makes strategy configuration accessible to non-technical users
- **Graceful fallback** — missing required fields fall back to full_refresh with a warning,
  never hard errors
- **Consistent pattern** — all write functions share the same catalog/namespace/fallback
  structure, easy to extend
- **Well-tested** — 11 new test cases for iceberg functions, 8 for config merge, 5 for
  executor dispatch, 6 for template helpers

### Negative

- **DuckDB in-memory SQL** for merge logic — all data passes through DuckDB RAM. For very
  large tables, this could be a memory concern. Future: consider PyIceberg-native merge
  operations when available.
- **No schema evolution** — SCD2 adds columns on first run but doesn't handle schema changes
  on subsequent runs (e.g., new columns in source data)
- **No partition pruning** — snapshot strategy filters by value, not by Iceberg partition
  metadata. Works but isn't optimal for very large tables.
- **Single-threaded writes** — each strategy runs in a single DuckDB connection. No parallel
  partition writes.

## Files Changed

| File | Changes |
|------|---------|
| `runner/models.py` | `MergeStrategy` class + 3 new `PipelineConfig` fields |
| `runner/config.py` | Parse new fields + `merge_configs()` function |
| `runner/templating.py` | 3 new annotation keys + 4 Jinja helpers |
| `runner/iceberg.py` | 4 new write functions (~200 lines) |
| `runner/executor.py` | Config merge logic + 6-strategy dispatch |
| `runner/tests/` | 30+ new test cases across 5 test files |
| `sdk-typescript/models/pipelines.ts` | `MergeStrategy` type + `PipelineConfig` interface |
| `portal/hooks/use-api.ts` | `usePipelineConfig()` hook |
| `portal/components/pipeline-merge-strategy.tsx` | New settings card component |
| `portal/components/code-editor.tsx` | Annotation schema + Jinja helper docs |
| `portal/app/pipelines/[ns]/[layer]/[name]/page.tsx` | Wire merge strategy component |
