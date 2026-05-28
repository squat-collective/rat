# ADR-009: ContainerExecutor — Per-Run Isolation (v2.6)

> **Update (2026-05-28):** RAT is now **100% free and open-source** — there are no Community/Pro editions or license keys. The Community/Pro framing in this record is **historical**; the capabilities it describes now ship as free, optional plugins.

## Status: Accepted

## Context

The community edition runs all pipelines in a **single shared runner container**
via the `WarmPoolExecutor` (ADR-003). This is simple and fast (~0 sec startup) but
provides no isolation between runs:

- All runs share the same S3 credentials (single MinIO admin)
- A misbehaving pipeline can exhaust memory/CPU and affect other runs
- A crash in one run can take down the entire runner process
- No per-namespace credential isolation for Pro multi-tenant scenarios

Pro users need **per-run isolation** for security (different S3 creds per namespace),
resource control (CPU/memory limits), and failure isolation (crash one, not all).

The executor plugin slot was designed in ADR-007 specifically for this use case.

## Decision

### ContainerExecutor Pro plugin

A new Pro plugin (`executor-container`) that spawns a **fresh runner container per
pipeline run** via the Podman REST API. It follows the same plugin pattern established
by auth-keycloak (ADR-008).

```
ratd (Go)                      executor-container (Pro plugin)        runner (single-shot)
┌─────────────────┐            ┌──────────────────────────┐           ┌──────────────────┐
│ PluginExecutor  │─ConnectRPC─│ ExecutorHandler           │           │ RUN_MODE=single  │
│  Submit()       │            │  Submit() → create+start  │──Podman──▶│ execute_pipeline │
│  Cancel()       │            │  GetRunStatus()→container │   API     │ print JSON result│
│  poll() loop    │            │  StreamLogs()→logs        │           │ exit 0/1         │
│                 │            │  Cancel()→kill             │           └──────────────────┘
└─────────────────┘            └──────────────────────────┘
```

### Runner single-shot mode

Rather than calling the runner's gRPC server from the Go plugin (which would require
waiting for gRPC startup, a gRPC client, polling `GetRunStatus`, and the container
staying alive), the runner gains a **single-shot mode** (`RUN_MODE=single`):

1. Reads pipeline config from env vars (`RUN_ID`, `NAMESPACE`, `LAYER`, `PIPELINE_NAME`, `TRIGGER`)
2. Calls `execute_pipeline()` directly — same execution logic (Nessie branches, DuckDB, quality tests)
3. Prints a JSON result line to stdout: `{"status":"success","rows_written":1234,"duration_ms":5678}`
4. Exits with code 0 (success) or 1 (failure)

Container exit code IS the outcome. Logs come from stdout. No gRPC overhead.

### Raw Podman API via Unix socket

The plugin communicates with the Podman API using **raw HTTP over a Unix socket** —
no external container SDK dependencies. This keeps the plugin image ~25MB (same as
auth-keycloak). Uses Podman libpod API v5.0.0:

- `POST /containers/create` + `POST /containers/{id}/start`
- `POST /containers/{id}/wait` (blocks until exit)
- `GET /containers/{id}/logs`
- `POST /containers/{id}/kill`
- `DELETE /containers/{id}`

### Community-side PluginExecutor adapter

A new `PluginExecutor` in the public repo (`platform/internal/executor/plugin.go`)
mirrors the `WarmPoolExecutor` pattern but talks to the `ExecutorService` plugin
via ConnectRPC instead of the runner directly.

In `main.go`, the plugin executor takes priority:

```go
if registry.ExecutorEnabled() {
    exec := executor.NewPluginExecutor(addr, srv.Runs)
    // ...
} else if runnerAddr := os.Getenv("RUNNER_ADDR"); runnerAddr != "" {
    exec := executor.NewWarmPoolExecutor(runnerAddr, srv.Runs)
    // ...
}
```

### Container lifecycle

- **Labels**: `rat.executor=container`, `rat.run-id={uuid}` — for tracking and orphan detection
- **Network**: Attached to compose network (`CONTAINER_NETWORK`) so runners reach MinIO/Nessie
- **Resource limits**: Configurable CPU (default 2.0 cores) + memory (default 1GB)
- **Reaper**: Background goroutine cleans exited containers after TTL (default 10min)
- **Orphan cleanup on startup**: Lists by label → kills running + removes all stale containers

### Compose overlay

Pro edition disables the warm runner (`replicas: 0`) and adds the executor-container
service. ratd's `rat.yaml` declares the executor plugin address.

## Consequences

### Positive

- **Full run isolation** — each run in its own container with its own env, creds, resources
- **Per-namespace S3 credentials** — `s3_config` map in `SubmitRequest` injects STS tokens per run
- **Resource limits** — CPU and memory limits per runner container (CFS quota)
- **Crash isolation** — one run crashing doesn't affect others
- **Same execution logic** — single-shot mode reuses 100% of existing `execute_pipeline()` code
- **No external deps** — raw HTTP to Podman API, ~25MB Docker image
- **Graceful degradation** — if executor plugin is unavailable, ratd falls back to WarmPoolExecutor
- **Clean startup** — orphan containers cleaned up on plugin restart
- **~40 new unit tests** across both repos

### Negative

- **Cold start overhead** — ~3-5 seconds per run to pull/create/start container (vs ~0 for warm pool)
- **Higher resource usage** — each runner container consumes memory even for small pipelines
- **Podman socket required** — the plugin needs access to the Podman socket (mounted as volume)
- **No log persistence** — logs are in-memory in the plugin; if plugin restarts, active run logs are lost. ratd detects stale runs via polling and marks them failed.

### File layout

#### Public repo (`rat/`)

```
proto/executor/v1/executor.proto           # ExecutorService definition
platform/gen/executor/v1/                  # Generated ConnectRPC code
platform/internal/executor/plugin.go       # PluginExecutor adapter
platform/internal/executor/plugin_test.go  # 8 tests
platform/internal/plugins/loader.go        # Extended with executor plugin type
platform/cmd/ratd/main.go                  # Plugin executor priority logic
runner/src/rat_runner/single_shot.py   # Single-shot mode
runner/src/rat_runner/__main__.py       # RUN_MODE dispatch
runner/tests/unit/test_single_shot.py          # 5 tests
```

#### Pro repo (`rat-pro/`)

```
plugins/executor-container/
├── cmd/executor-container/main.go          # Config → runtime → handlers → HTTP server
├── internal/
│   ├── config/config.go                    # Env vars (16 fields)
│   ├── handler/executor.go                 # ExecutorService handler (core logic)
│   ├── handler/plugin.go                   # PluginService.HealthCheck
│   └── runtime/
│       ├── runtime.go                      # ContainerRuntime interface + PodmanRuntime
│       └── reaper.go                       # Background cleanup of exited containers
├── go.mod, Dockerfile, Makefile
infra/docker-compose.pro.yml                # executor-container service + runner disabled
infra/rat.yaml                              # executor plugin address
infra/test-executor.sh                      # E2E integration test
```

### Environment variables (executor-container plugin)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GRPC_PORT` | No | `50070` | Plugin server port |
| `RUNNER_IMAGE` | **Yes** | — | Image for spawned runners |
| `PODMAN_SOCKET` | No | `/run/podman/podman.sock` | Podman API socket |
| `CONTAINER_NETWORK` | No | `infra_default` | Docker/Podman network |
| `CONTAINER_CPU_LIMIT` | No | `2.0` | CPU cores per runner |
| `CONTAINER_MEMORY_LIMIT` | No | `1073741824` | Memory bytes (1GB) |
| `REAPER_INTERVAL` | No | `60s` | Cleanup sweep interval |
| `CONTAINER_TTL` | No | `600s` | Keep exited containers for |
| `S3_ENDPOINT` | **Yes** | — | Injected into runners |
| `S3_ACCESS_KEY` | **Yes** | — | Injected into runners |
| `S3_SECRET_KEY` | **Yes** | — | Injected into runners |
| `S3_BUCKET` | No | `rat` | Injected into runners |
| `NESSIE_URL` | **Yes** | — | Injected into runners |

### Error handling

| Scenario | Behavior |
|----------|----------|
| Podman socket unreachable | Plugin stays NOT_SERVING → ratd falls back to WarmPoolExecutor |
| Container create/start fails | Submit returns error → ratd marks run FAILED |
| Container exits non-zero | waitForCompletion updates status to FAILED with error |
| Plugin crash mid-run | Orphan cleanup on restart; ratd poll gets "unknown run" → marks FAILED |
| Network unreachable from runner | Runner can't reach MinIO/Nessie → exits 1 → normal failure |
