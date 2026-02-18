# ADR-005: Runner Service Architecture

## Status: Accepted

## Context

The WarmPoolExecutor (ADR-003) dispatches pipeline runs to `runner:50052` via ConnectRPC, polls
status every 5s. The runner is a Python service responsible for actual pipeline execution:
reading SQL from S3, compiling Jinja templates, executing via DuckDB, and writing results to
Iceberg tables via Nessie.

Key requirements:
- Execute SQL pipelines with full refresh (MVP scope)
- Read pipeline code from S3, write results to Iceberg
- Report status (PENDING → RUNNING → SUCCESS/FAILED) back to ratd via gRPC
- Stream logs in real-time for the portal's run log viewer
- Support cancellation of in-flight runs
- Handle multiple concurrent runs (ratd can submit N pipelines)

## Decision

### Sync gRPC (not async)

Use synchronous `grpcio` server, not `grpc.aio`. DuckDB operations are CPU-bound and blocking —
async adds complexity (executor bridging, event loop management) with no throughput benefit.

### Thread pool architecture

Two separate thread pools:
- **gRPC server pool** (10 workers): handles RPC requests (SubmitPipeline, GetRunStatus, etc.)
- **Pipeline execution pool** (4 workers): runs `execute_pipeline()` in dedicated threads

This separation ensures gRPC remains responsive even when all 4 pipeline workers are busy.

### One DuckDB connection per run

DuckDB connections are not thread-safe. Each `execute_pipeline()` call creates a fresh in-memory
connection with httpfs + iceberg extensions configured for S3 access. Connection is closed in
the `finally` block regardless of outcome.

### In-memory run registry

All run state lives in `dict[str, RunState]`. No persistence — ratd owns the source of truth
in Postgres. If the runner restarts, ratd detects stale `running` runs via polling and can
re-submit or mark them failed.

This simplifies the runner dramatically: no database, no state recovery, no distributed
consensus. The tradeoff is that runner restart loses in-progress runs, which is acceptable
for Community edition (single warm runner).

### Log collection via bounded deque

Each `RunState` holds a `deque(maxlen=10_000)` of `LogRecord` entries. The `RunLogger` writes
to both the deque (for gRPC `StreamLogs`) and Python's `logging` module (for container stdout).

Bounded deque prevents OOM on chatty pipelines. 10K entries at ~200 bytes each ≈ 2MB per run,
with 4 concurrent runs ≈ 8MB total — negligible.

### Cancellation via threading.Event

Each `RunState` has a `cancel_event: threading.Event`. The executor checks this between phases
(S3 read → compile → DuckDB execute → Iceberg write). When set, raises `CancelledError` which
is caught and sets status to CANCELLED.

This is cooperative cancellation — a long-running DuckDB query cannot be interrupted mid-execution.
For MVP this is acceptable; future improvement could use DuckDB's interrupt API.

### SQL templating with ref() resolution

Jinja2 templates with custom functions:
- `ref('layer.name')` → resolves to `read_parquet('s3://bucket/ns/layer/name/**/*.parquet')`
- `ref('ns.layer.name')` → cross-namespace reference
- `this` → current pipeline's target identifier
- `run_started_at` → ISO timestamp
- `is_incremental()` → always `False` for MVP

For MVP, `ref()` resolves to parquet glob reads. Future versions will resolve to
`iceberg_scan()` once Iceberg metadata paths are standardized.

### Pipeline execution flow

```
execute_pipeline(run, s3_config, nessie_config):
  1. Read pipeline.sql from S3:  {ns}/pipelines/{layer}/{name}/pipeline.sql
  2. Read config.yaml from S3:   {ns}/pipelines/{layer}/{name}/config.yaml  (optional)
  3. Compile SQL:                 Jinja rendering + ref() resolution
  4. Execute via DuckDB:          query_arrow(compiled_sql) → pa.Table
  5. Write to Iceberg:            PyIceberg overwrite → {ns}.{layer}.{name}
  6. Update RunState:             SUCCESS (with rows_written, duration_ms)

  On error at any phase → FAILED with error message
  On cancel between phases → CANCELLED
```

### S3/Nessie config from environment

The runner reads its own S3 and Nessie configuration from environment variables, not from the
`SubmitPipelineRequest.s3_config` map. This simplifies security (no credential passing over
gRPC) and matches how ratd/ratq are configured.

Future: per-run STS credentials passed via `s3_config` map for multi-tenant isolation.

## Consequences

### Positive
- Simple, synchronous, easy to debug — no async complexity
- Stateless (from runner's perspective) — ratd is the source of truth
- 4 concurrent pipelines without contention (separate DuckDB connections)
- Real-time log streaming via deque cursor + follow mode
- 77 unit tests, 89% coverage — well-tested from day one
- Clean separation: models → config → engine → templating → iceberg → executor → server

### Negative
- In-memory state lost on restart (acceptable — ratd detects and handles)
- Cooperative cancellation only (can't interrupt mid-query)
- `ref()` resolves to parquet glob, not Iceberg scan (MVP limitation)
- Fixed 4-worker pool (not configurable via env var yet)
- No health check endpoint (gRPC channel-ready check used instead)

## Implementation

```
runner/src/rat_runner/
├── __main__.py      # Entrypoint: sys.path setup, logging config, serve()
├── server.py        # RunnerServiceImpl (4 RPCs) + serve() function
├── executor.py      # execute_pipeline() — the core loop
├── engine.py        # DuckDBEngine — lazy connection, S3 extensions
├── templating.py    # Jinja SQL — ref(), this, run_started_at, metadata
├── iceberg.py       # PyIceberg writes — get_catalog, ensure_namespace, write_iceberg
├── config.py        # S3Config, NessieConfig, parse_pipeline_config, read_s3_text
├── models.py        # RunState, RunStatus, LogRecord, PipelineConfig
├── log.py           # RunLogger — dual output (deque + Python logging)
└── gen/             # Generated gRPC stubs (unchanged)

runner/tests/
├── conftest.py
└── unit/
    ├── test_models.py      # 12 tests
    ├── test_config.py      # 17 tests
    ├── test_engine.py      # 5 tests
    ├── test_templating.py  # 11 tests
    ├── test_iceberg.py     # 7 tests
    ├── test_log.py         # 3 tests
    ├── test_executor.py    # 9 tests
    └── test_server.py      # 8 tests (in-process gRPC)
```

Total: 10 source files, 77 unit tests, 89% coverage.

---

## Addendum: v2.1 Feature Expansion

> Added Feb 2026. The original decision stands — this documents the scope expansion.

### New Capabilities

Six deferred features were implemented on top of the MVP architecture:

1. **Per-Run STS Credentials** — `S3Config.with_overrides()` merges `request.s3_config` map (Pro: per-user STS tokens)
2. **Run Cleanup** — background daemon thread evicts terminal runs past `RUN_TTL_SECONDS` (default 3600)
3. **Incremental Pipelines** — `merge_iceberg()` (ANTI JOIN + UNION ALL with QUALIFY dedup), `read_watermark()`, `is_incremental()` Jinja function now config-driven
4. **Python Pipelines** — `exec()` with injected globals (`duckdb_conn`, `pa`, `ref`, `this`, `run_started_at`, `is_incremental`, `config`). Must set `result = pa.Table(...)`
5. **Ephemeral Nessie Branches** — each run creates `run-{run_id}` branch, writes there, merges to main on success or deletes on quality failure
6. **Quality Tests** — discovers `tests/quality/*.sql` from S3, compiles via Jinja, executes in DuckDB (0 rows = pass), respects `-- @severity: error|warn`

### Updated Execution Flow

```
execute_pipeline(run, s3_config, nessie_config):
  Phase 0: Create ephemeral Nessie branch "run-{run_id}" (falls back to main)
  Phase 1: Detect pipeline type (.py first, then .sql), read config.yaml
  Phase 2: Build result pa.Table
           SQL:    watermark read (if incremental) → Jinja compile → DuckDB
           Python: exec() with globals → extract `result`
  Phase 3: Write to Iceberg on ephemeral branch
           full_refresh → write_iceberg (overwrite)
           incremental  → merge_iceberg (ANTI JOIN + UNION ALL)
  Phase 4: Quality tests on ephemeral branch
           Discover tests/quality/*.sql → compile → execute → 0 rows = pass
  Phase 5: Branch resolution
           Any error-severity fail → delete branch, FAILED
           All pass / no tests     → merge branch to main, SUCCESS
  Finally: delete branch, restore env, close engine, record duration
```

### Updated Template Functions

- `ref('layer.name')` → parquet glob read (unchanged)
- `this` → current pipeline target identifier (unchanged)
- `run_started_at` → ISO timestamp (unchanged)
- `is_incremental()` → `True` when `config.merge_strategy == "incremental"` (was hardcoded False)
- `watermark_value` → max value of `watermark_column` from existing Iceberg table (new)

### New Modules

```
runner/src/rat_runner/
├── ... (original 10 files)
├── nessie.py        # Nessie v2 REST client (create/merge/delete branch)
├── python_exec.py   # Python pipeline execution via exec()
└── quality.py       # Quality test discovery, execution, severity parsing
```

### Updated Stats

Total: 13 source files, 140 unit tests, 91% coverage.

---

## Addendum: v2.11 Merge Strategies

> Added Feb 2026. Expands write modes from 2 to 6 strategies. See ADR-014 for full decision.

### Config Merge (Breaking Change)

Previously, if ANY source annotation existed, `config.yaml` was entirely ignored. Now both
sources are merged per-field:

```
config.yaml (base) + annotations (overrides) → PipelineConfig
```

This enables the portal to manage `config.yaml` while users override specific fields with annotations.

### 4 New Write Functions

| Function | Strategy | DuckDB SQL Pattern |
|----------|----------|--------------------|
| `append_iceberg()` | `append_only` | `table.append(data)` — pure PyIceberg, no SQL |
| `delete_insert_iceberg()` | `delete_insert` | ANTI JOIN + UNION ALL (no dedup) |
| `scd2_iceberg()` | `scd2` | Close matching, keep historical, add new with valid_from/to |
| `snapshot_iceberg()` | `snapshot` | `WHERE partition NOT IN (new) UNION ALL new_data` |

All follow the existing `write_iceberg()`/`merge_iceberg()` pattern: get_catalog →
ensure_namespace → load_table → DuckDB merge → `table.overwrite()`. `NoSuchTableError`
falls back to `write_iceberg()` (create on first run).

### New PipelineConfig Fields

```python
partition_column: str = ""           # for snapshot
scd_valid_from: str = "valid_from"   # for scd2
scd_valid_to: str = "valid_to"       # for scd2
```

### New Template Functions

- `is_append_only()` → `True` when strategy is `append_only`
- `is_delete_insert()` → `True` when strategy is `delete_insert`
- `is_scd2()` → `True` when strategy is `scd2`
- `is_snapshot()` → `True` when strategy is `snapshot`

### Updated Execution Flow (Phase 3)

```
Phase 3: Write to Iceberg on ephemeral branch
         full_refresh              → write_iceberg (overwrite)
         incremental + unique_key  → merge_iceberg (ANTI JOIN + dedup)
         append_only               → append_iceberg (pure append)
         delete_insert + unique_key → delete_insert_iceberg (ANTI JOIN, no dedup)
         scd2 + unique_key         → scd2_iceberg (history tracking)
         snapshot + partition_col   → snapshot_iceberg (partition-aware)
         missing required field     → WARNING + write_iceberg (fallback)
```

### Updated Stats

Total: 13 source files, ~292 unit tests.
