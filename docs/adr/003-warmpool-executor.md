# ADR-003: WarmPoolExecutor for Pipeline Dispatch

## Status: Accepted

## Context

When a pipeline run is created (via API or scheduler), ratd needs to dispatch it to the Python
runner service for execution. The executor is a plugin slot — Community ships with a default,
Pro can swap in ContainerExecutor, LambdaExecutor, K8sJobExecutor, etc.

Key requirements:
- Dispatch runs to the runner via ConnectRPC (gRPC-compatible)
- Track in-flight runs and poll for completion
- Update Postgres run status as the run progresses (pending → running → success/failed)
- Graceful degradation: runs are created even if the executor fails to dispatch
- Must be testable with mock runner client

## Decision

### Executor interface

Defined in `api/executor.go` to avoid import cycles:

```go
type Executor interface {
    Submit(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error
    Cancel(ctx context.Context, runID string) error
}
```

### WarmPoolExecutor

Community's default executor connects to a single pre-started runner sidecar. The runner
is a long-running Python process (warm pool of 1), not ephemeral.

```go
type WarmPoolExecutor struct {
    runner       runnerv1connect.RunnerServiceClient  // ConnectRPC client
    runs         api.RunStore                         // DB access for status updates
    mu           sync.Mutex
    active       map[string]*domain.Run               // in-flight runs
    pollInterval time.Duration                        // 5s default
    cancel       context.CancelFunc
    done         chan struct{}
}
```

### Submit flow

1. Build `SubmitPipelineRequest` from domain types (namespace, layer, pipeline name, trigger)
2. Call `runner.SubmitPipeline()` via ConnectRPC
3. **Success**: update run to `running`, add to active map
4. **Failure**: update run to `failed` with error message, return error

### Status polling

Background goroutine polls `GetRunStatus` every 5 seconds for all active runs:
- Terminal status (`success`/`failed`): update DB, remove from active map
- Non-terminal: continue polling
- Runner unreachable: log warning, retry on next tick

### Cancel flow

1. Call `runner.CancelRun()` via ConnectRPC
2. Remove from active map
3. Run status update to `cancelled` is handled by the API handler (before calling executor)

### Integration with API handlers

`HandleCreateRun` (POST /api/v1/runs):
```go
// After creating run in DB...
if s.Executor != nil {
    if err := s.Executor.Submit(r.Context(), run, pipeline); err != nil {
        slog.Error("executor submit failed", "run_id", run.ID, "error", err)
    }
}
```

Best-effort dispatch — the run exists in DB regardless of executor outcome.

### Why ConnectRPC?

- HTTP/1.1 compatible (works through standard proxies)
- curl-friendly for debugging (`application/json` mode)
- Generated client interface (`RunnerServiceClient`) is easy to mock in tests
- Same proto definitions used by runner (Python) and ratd (Go)

### Why polling instead of streaming?

- Simpler implementation — no long-lived gRPC stream to maintain
- 5s poll interval is acceptable for batch pipeline runs (not real-time)
- Log streaming (SSE) is handled separately by the API layer, not the executor

## Consequences

### Positive
- Zero-startup-time execution — runner is always warm (~0 sec dispatch)
- Clean separation — executor interface allows swapping implementations
- Resilient — runs are persisted before dispatch, executor failure doesn't lose runs
- Testable — 8 unit tests with mock runner client
- Domain ↔ Proto conversion is explicit and typed (`domainLayerToProto`, `protoStatusToDomain`)

### Negative
- Single runner = no parallelism (one run at a time for Community). Pro's ContainerExecutor fixes this.
- Polling adds latency to status detection (up to 5s delay)
- Active run map is in-memory — ratd restart loses tracking (runs stay `running` in DB). A recovery mechanism can be added later.

## Implementation

- `platform/internal/api/executor.go` — `Executor` interface
- `platform/internal/executor/warmpool.go` — `WarmPoolExecutor` struct + Submit/Cancel/Start/Stop/poll
- `platform/internal/executor/warmpool_test.go` — 8 tests with mock runner client
- `platform/internal/api/runs.go` — executor dispatch in HandleCreateRun + HandleCancelRun
- `platform/cmd/ratd/main.go` — wired when `RUNNER_ADDR` env var is set
