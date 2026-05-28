# rat-runner

The Python service that executes RAT pipelines.

Receives a pipeline run dispatch from `ratd` (via ConnectRPC), reads the
pipeline source from S3, executes it against DuckDB, writes results to
an Iceberg table via PyIceberg, and reports status back. Each run is
isolated on a per-run Nessie branch that's merged into `main` on
success — so a failed run never poisons the production catalog.

## Architecture

```
        ratd
          │ SubmitPipeline (ConnectRPC)
          ▼
    ┌───────────┐
    │  runner   │  ─► S3 (read pipeline.sql / pipeline.py / config.yaml)
    │           │  ─► Nessie (create branch, merge on success)
    │           │  ─► DuckDB (execute SQL or Python sandbox)
    │           │  ─► PyIceberg + S3 (write parquet + metadata)
    └───────────┘
```

Two execution modes:

- **`RUN_MODE=server`** (default) — long-lived gRPC server, picks up
  dispatches as they arrive. This is the Community-Edition mode used by
  `WarmPoolExecutor`.
- **`RUN_MODE=single`** — single-shot: read run params from env, execute
  once, print JSON result to stdout, exit. Used by the Pro
  `ContainerExecutor` plugin (one container per run, no warm pool).

## Pipeline types

| Type | Extension | Execution |
|---|---|---|
| `sql` | `pipeline.sql` | Jinja-rendered SQL → DuckDB `execute()` → Arrow table |
| `python` | `pipeline.py` | Sandboxed `exec()` of a `pipeline()` function returning a `pyarrow.Table` |

The Python sandbox blocklist (`python_exec.py`) is **defense-in-depth
only** — the real trust boundary is the runner container itself (read-
only fs, dropped caps, no-new-privileges). See [ADR-017](../docs/adr/017-python-pipeline-trust-model.md).

## Merge strategies

Six core strategies (in `runner/src/rat_runner/strategies/`):

| Strategy | Behaviour | Required fields |
|---|---|---|
| `full_refresh` | Replace whole table each run | — |
| `incremental` | Merge new rows by `unique_key`; deduplicate via `ROW_NUMBER()` | `unique_key`, optional `watermark_column` |
| `append_only` | Always append, never overwrite | — |
| `delete_insert` | Delete matching `unique_key`, insert all new rows | `unique_key` |
| `scd2` | SCD Type 2 with `valid_from` / `valid_to` columns | `unique_key`, `scd_valid_from`, `scd_valid_to` |
| `snapshot` | Replace only partitions present in the new data | `partition_column` |

Plugin-supplied strategies (e.g. [`rat-plugin-soft-delete`](../plugins/rat-plugin-soft-delete/))
add to this set via the `rat.strategies` entry-point.

## Plugin entry points

The runner discovers plugins through Python's `entry_points`. Five groups:

| Group | What it provides |
|---|---|
| `rat.strategies` | New merge strategies (e.g. `soft_delete`) |
| `rat.pipeline_types` | New pipeline languages (e.g. `prql`) |
| `rat.sources` | New input source connectors (e.g. `http`) |
| `rat.hooks` | Post-write hooks (e.g. `row_stats`) |
| `rat.jinja_helpers` | Extra Jinja functions for SQL templates (e.g. dbt-compat) |

Install a plugin by adding it to the runner's pyproject deps or
mounting it into `/plugins`. The runner auto-installs anything in
`/plugins` on startup.

## Quality tests

Each pipeline can ship SQL assertions under `tests/quality/*.sql`. The
runner discovers and executes them after the pipeline run; any test
that returns a non-zero row count fails the run. Bounded per-test by
`QUALITY_TEST_TIMEOUT_SECS` (default 60s).

## Configuration

All config via env vars — see [`docs/config.md`](../docs/config.md#runner-service-pipeline-execution)
for the full table. The most common knobs:

| Var | Default | Purpose |
|---|---|---|
| `RUNNER_ADDR` (on `ratd`) | — | When set, `ratd` dispatches to this runner |
| `GRPC_PORT` | `50052` | Server listen port |
| `QUERY_TIMEOUT_SECS` | `60` | Per-query DuckDB timeout |
| `QUALITY_TEST_TIMEOUT_SECS` | `60` | Per-quality-test timeout |
| `RUN_TTL_SECONDS` | `3600` | TTL for terminal runs in the in-memory registry |
| `NESSIE_URL`, `S3_*` | — | Catalog + object store config (mirrors ratd's) |

## Development

```bash
# from repo root
make test-py        # runs runner + query test suites in Docker
make dev-runner     # hot-reload Python container with code mount
make proto          # regenerate gRPC stubs (if proto/ changed)
```

The Python test suite is in `tests/unit/` (~560 tests, ~91% coverage).

## See also

- [`docs/adr/005-runner-service.md`](../docs/adr/005-runner-service.md) — original architecture decision
- [`docs/adr/014-merge-strategies.md`](../docs/adr/014-merge-strategies.md) — strategy framework
- [`docs/adr/017-python-pipeline-trust-model.md`](../docs/adr/017-python-pipeline-trust-model.md) — sandbox trust boundary
- [`docs/PLUGIN_AUTHOR_GUIDE.md`](../docs/PLUGIN_AUTHOR_GUIDE.md) — how to write a runner plugin
