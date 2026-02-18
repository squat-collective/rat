# ADR-004: Background Cron Scheduler

## Status: Accepted

## Context

Users configure cron schedules for pipelines via the API (`POST /api/v1/schedules`). These
schedules need to be evaluated periodically to fire pipeline runs at the right time.

Key requirements:
- Evaluate cron expressions (5-field: minute, hour, dom, month, dow)
- Fire runs when `next_run_at <= now()`
- Handle missed schedules (ratd restart, long downtime) — catch up once, don't backfill
- Compute and persist `next_run_at` for each schedule
- Handle edge cases: disabled schedules, nil `next_run_at`, invalid cron expressions
- Must not block the API — runs as a background goroutine

## Decision

### Scheduler struct

```go
type Scheduler struct {
    schedules api.ScheduleStore
    pipelines api.PipelineStore
    runs      api.RunStore
    executor  api.Executor
    interval  time.Duration   // 30s default
    parser    cron.Parser     // robfig/cron/v3
    cancel    context.CancelFunc
    done      chan struct{}
}
```

### Tick cycle (every 30 seconds)

1. Load all schedules from Postgres (`ListSchedules`)
2. For each enabled schedule:
   - **Invalid cron**: log warning, skip
   - **`next_run_at` is nil**: compute from `now()`, persist, don't fire (first-time setup)
   - **`next_run_at` in future**: skip (not due)
   - **`next_run_at <= now()`**: fire the run

3. Firing a run:
   a. Look up pipeline by ID (`GetPipelineByID`)
   b. Create run with trigger `"schedule:<cron_expr>"`
   c. Submit to executor
   d. Compute next run time from `now()` (not from `next_run_at`)
   e. Update schedule with `last_run_id`, `last_run_at`, `next_run_at`

### Missed schedule handling

**Decision**: Catch up once, then advance to future.

If ratd was down for 3 hours and a schedule has `next_run_at` 3 hours ago:
- Fire exactly **one** run (not 3 catchup runs)
- Compute `next_run_at` from `now()`, not from the missed time

**Rationale**: Backfilling all missed runs is dangerous (could trigger 100s of runs on restart).
Single catchup + advance to future is the safest default. Backfill can be triggered manually.

### Why `robfig/cron/v3`?

- Battle-tested (17k+ GitHub stars)
- Standard 5-field cron (matches user expectations)
- `Parser` is configurable (we use `Minute | Hour | Dom | Month | Dow`)
- `Schedule.Next(time.Time)` computes next occurrence — no custom logic needed
- Tiny dependency (~1 file, no transitive deps)

### Why 30-second interval?

- Cron's minimum resolution is 1 minute — 30s ensures we never miss a minute boundary
- Low overhead — one DB query per tick, no active schedules = no work
- Configurable via constructor (not env var for now, but easy to add)

### Why scheduler starts only when executor is available?

```go
if srv.Executor != nil {
    sched := scheduler.New(...)
    sched.Start(ctx)
}
```

A schedule without an executor is useless — it would create runs that stay `pending` forever.
By gating on executor availability, we ensure the scheduler only runs when it can actually
dispatch work.

### New SQL queries required

- `GetPipelineByID` — scheduler needs to look up pipeline details by UUID (existing queries use namespace/layer/name)
- `UpdateScheduleRun` — atomic update of `last_run_id`, `last_run_at`, `next_run_at` on a schedule

Both were added to the `PipelineStore` and `ScheduleStore` interfaces.

## Consequences

### Positive
- Simple, predictable behavior — one goroutine, one ticker, one DB query per tick
- Missed schedules handled safely (catch up once, no storm)
- Run trigger is traceable: `"schedule:0 * * * *"` in the run record
- 8 unit tests cover all edge cases (no schedules, disabled, due, future, nil next_run_at, missed, executor failure, invalid cron)
- No external dependencies beyond robfig/cron

### Negative
- 30-second granularity means up to 30s delay on schedule fires (acceptable for batch pipelines)
- No distributed locking — if multiple ratd instances run, schedules could fire twice. Single-instance only for now.
- `ListSchedules` loads all schedules every tick — for large numbers of schedules, a `WHERE enabled = true AND next_run_at <= now()` query would be more efficient

## Implementation

- `platform/internal/scheduler/scheduler.go` — `Scheduler` struct + Start/Stop/tick
- `platform/internal/scheduler/scheduler_test.go` — 8 tests with mock stores
- `platform/internal/api/schedules.go` — `UpdateScheduleRun` added to `ScheduleStore` interface
- `platform/internal/api/pipelines.go` — `GetPipelineByID` added to `PipelineStore` interface
- `platform/internal/postgres/schedule_store.go` — `UpdateScheduleRun` implementation
- `platform/internal/postgres/pipeline_store.go` — `GetPipelineByID` implementation
- `platform/internal/postgres/queries/schedules.sql` — `UpdateScheduleRun` SQL
- `platform/internal/postgres/queries/pipelines.sql` — `GetPipelineByID` SQL
- `platform/cmd/ratd/main.go` — wired when executor is available
