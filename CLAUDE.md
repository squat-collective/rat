# 🐀 RAT — Development Guidelines

> *"In the sewer, we transform data faster than anyone above ground."*

## About

RAT is a self-hostable data platform. Community Edition is free, open-source, single-user.
Pro Edition adds multi-user, sharing, and cloud via closed-source container plugins.

**v2 is a full rewrite**: Go platform (`ratd`) + Python execution (runner, query) + Next.js portal.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    docker compose up                              │
│                                                                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │  ratd    │  │  ratq    │  │  runner   │  │  portal          │ │
│  │  (Go)    │  │  (Python)│  │  (Python) │  │  (Next.js)       │ │
│  │  API     │  │  DuckDB  │  │  Pipeline │  │  Web IDE         │ │
│  │  Sched   │◄─┤  Queries │  │  Execution│  │  Editor/Query    │ │
│  │  Plugins │  │  Read    │  │  Write    │  │  Pipelines/DAG   │ │
│  └────┬─────┘  └──────────┘  └───────────┘  └──────────────────┘ │
│       │                                                           │
│  ┌────┴─────┐  ┌──────────┐  ┌──────────┐                       │
│  │ postgres │  │  minio   │  │  nessie   │                       │
│  └──────────┘  └──────────┘  └──────────┘                       │
└─────────────────────────────────────────────────────────────────┘
```

| Service | Language | Role |
|---------|----------|------|
| **ratd** | Go | API server, auth, scheduling, plugin host, catalog, storage ops |
| **ratq** | Python | Interactive DuckDB queries (read-only), schema introspection |
| **runner** | Python | Pipeline execution, DuckDB writes, PyArrow, Iceberg, quality tests |
| **portal** | Next.js | Web IDE — the ONLY user interface |
| **postgres** | — | Platform state (runs, schedules, ownership) |
| **minio** | — | S3-compatible object storage |
| **nessie** | — | Iceberg REST catalog |

### Communication

- Portal → ratd: **REST** (HTTP)
- ratd → ratq: **gRPC** (ConnectRPC)
- ratd → runner: **gRPC** (ConnectRPC)
- ratd → plugins: **gRPC** (ConnectRPC)
- ratd → postgres: **SQL**
- ratd → minio: **S3 API**
- ratd → nessie: **Iceberg REST API**

---

## Repository Structure

```
~/sandbox/ratatouille-v2/              # umbrella (not a repo)
├── ratatouille/                       # PUBLIC community monorepo
│   ├── platform/                      # Go — ratd
│   ├── runner/                        # Python — pipeline execution
│   ├── query/                         # Python — DuckDB query sidecar
│   ├── portal/                        # Next.js — web IDE
│   ├── sdk-typescript/                # TS SDK for portal
│   ├── proto/                         # Shared gRPC protobuf definitions
│   ├── infra/                         # Docker compose, configs, scripts
│   ├── docs/                          # Architecture docs, ADRs
│   ├── Makefile                       # Root orchestrator
│   └── CLAUDE.md                      # This file
│
└── ratatouille-pro/                   # PRIVATE pro plugins (separate git repo)
    ├── plugins/
    │   └── auth-keycloak/             # Keycloak JWT validation (Go, ~26MB)
    ├── portal-pro/                    # Pro portal extension
    └── infra/                         # Pro compose overrides + Keycloak realm
```

---

## Git Workflow — GitHub Flow

### Branch Naming

```
main                              # always deployable, protected
feat/ratd-api-endpoints           # new feature
fix/runner-duckdb-memory-leak     # bug fix
refactor/proto-message-cleanup    # refactoring
docs/adr-executor-plugin          # documentation
test/ratq-integration-tests       # test additions
```

### Rules

1. **`main` is always green** — never push broken code to main
2. **Feature branches are short-lived** — days, not weeks. Small PRs.
3. **Every PR requires**:
   - Tests passing (CI green)
   - At least the relevant component's tests run
   - Clear PR description with "why", not just "what"
4. **Squash merge** to main — clean linear history
5. **Delete branches** after merge
6. **No force push to main** — ever
7. **Tag releases** with semver: `v2.0.0`, `v2.1.0`, etc.

### Commit Messages

```
<type>(<scope>): <description>

feat(ratd): add pipeline CRUD endpoints
fix(runner): handle DuckDB OOM on large datasets
test(ratq): add integration tests for schema introspection
refactor(proto): rename PipelineSpec to PipelineManifest
docs(adr): document executor plugin decision
chore(infra): update postgres to 16.2
```

Types: `feat`, `fix`, `test`, `refactor`, `docs`, `chore`, `ci`
Scopes: `ratd`, `runner`, `ratq`, `portal`, `proto`, `sdk`, `infra`

---

## TDD — Test-Driven Development

### The Cycle

```
1. RED    — Write a failing test that defines desired behavior
2. GREEN  — Write the minimum code to make it pass
3. REFACTOR — Clean up while keeping tests green
```

### Rules

- **Write the test FIRST** — no exceptions. If you can't test it, rethink the design.
- **Every bug fix gets a regression test** — after fixing a bug, always add a test that would have caught it. No fix is complete without a test proving the bug is dead.
- **One assertion per test** (where reasonable) — tests should fail for exactly one reason.
- **Tests are documentation** — test names describe behavior, not implementation.
- **No mocks unless necessary** — prefer real dependencies (test containers, in-memory DBs). Mock external services only.
- **Test at the right level**:
  - **Unit tests**: pure functions, business logic, parsers, validators
  - **Integration tests**: API endpoints, gRPC services, database queries
  - **E2E tests**: full pipeline run through all services (fewer, slower)

### Test Structure

```
# Go (platform/)
platform/
├── internal/api/
│   ├── handler.go
│   └── handler_test.go          # tests live next to code
├── internal/executor/
│   ├── executor.go
│   └── executor_test.go
└── integration_test/            # cross-package integration tests
    └── api_integration_test.go

# Python (runner/, query/)
runner/
├── src/rat_runner/
│   ├── engine.py
│   └── ...
└── tests/
    ├── unit/
    │   ├── test_engine.py
    │   └── test_templating.py
    └── integration/
        └── test_pipeline_run.py

# TypeScript (portal/, sdk-typescript/)
portal/
├── src/
└── tests/                       # or __tests__/ colocated
    ├── components/
    └── hooks/
```

### Naming Convention

```python
# Python — descriptive, reads like a sentence
def test_pipeline_run_writes_results_to_iceberg():
def test_ref_resolution_raises_on_missing_table():
def test_quality_test_fails_on_null_primary_key():

# Go — TestFunction_Scenario_ExpectedBehavior
func TestSubmitPipeline_ValidSpec_ReturnsRunHandle(t *testing.T)
func TestSubmitPipeline_InvalidLayer_ReturnsError(t *testing.T)

# TypeScript — describe/it blocks
describe("useQuery", () => {
  it("returns results when query succeeds", () => {})
  it("shows error when query fails", () => {})
})
```

### Coverage

- **Target: 80%+ on core logic** (engine, executor, auth, API handlers)
- **Don't chase 100%** — skip trivial getters, config loading, boilerplate
- **CI enforces coverage** — PRs that drop below threshold are blocked
- **Integration tests count** — a good integration test is worth 10 unit tests

---

## Go Guidelines (platform/)

### Version & Tooling

- **Go 1.22+**
- **Router**: `chi` (lightweight, stdlib-compatible)
- **gRPC**: ConnectRPC (`connectrpc.com/connect`) + protobuf (`google.golang.org/protobuf`)
- **Cron**: `github.com/robfig/cron/v3` (5-field cron parsing for scheduler)
- **S3**: MinIO Go SDK (`github.com/minio/minio-go/v7`)
- **Database**: `pgx` (pure Go Postgres driver) + `sqlc` (type-safe SQL)
- **Config**: `envconfig` or `koanf` (env vars + yaml)
- **Logging**: `slog` (stdlib structured logging)
- **Testing**: stdlib `testing` + `testify` for assertions

### Code Style

```go
// DO: short, focused functions
func (s *PipelineService) Create(ctx context.Context, req *CreatePipelineRequest) (*Pipeline, error) {
    if err := req.Validate(); err != nil {
        return nil, connect.NewError(connect.CodeInvalidArgument, err)
    }
    // ...
}

// DO: errors are values, handle them explicitly
result, err := s.store.GetPipeline(ctx, id)
if err != nil {
    return nil, fmt.Errorf("get pipeline %s: %w", id, err)
}

// DO: use context for cancellation and timeouts
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

// DON'T: naked returns, global state, init() functions
// DON'T: panic in library code (only in main for truly unrecoverable)
// DON'T: interface pollution — define interfaces where they're consumed, not produced
```

### Package Layout

```
platform/
├── cmd/ratd/
│   └── main.go                    # wiring only, no logic
├── internal/
│   ├── api/                       # HTTP handlers (chi routes)
│   │   ├── router.go              # Server struct, route registration, middleware
│   │   ├── health.go              # GET /health + GET /features
│   │   ├── pipelines.go           # pipeline CRUD (5 endpoints)
│   │   ├── runs.go                # run CRUD + cancel + SSE logs (5 endpoints)
│   │   ├── namespaces.go          # namespace CRUD (3 endpoints)
│   │   ├── schedules.go           # schedule CRUD (5 endpoints)
│   │   ├── storage.go             # S3 file operations + upload (5 endpoints)
│   │   ├── quality.go             # quality test management (4 endpoints)
│   │   ├── metadata.go            # .meta.yaml reads (2 endpoints)
│   │   ├── query.go               # DuckDB query proxy (4 endpoints)
│   │   ├── executor.go            # Executor interface (Submit + Cancel)
│   │   ├── *_test.go              # tests colocated with handlers
│   │   └── testhelpers_test.go    # shared in-memory stores for tests
│   ├── auth/                      # auth middleware plugin slot
│   │   ├── middleware.go          # Noop() — community pass-through
│   │   └── middleware_test.go     # 3 tests
│   ├── config/                    # rat.yaml config loading
│   │   ├── config.go              # Config, PluginConfig, Load(), ResolvePath()
│   │   └── config_test.go         # 10 tests
│   ├── executor/                  # pipeline dispatch to runner
│   │   ├── warmpool.go            # WarmPoolExecutor — ConnectRPC dispatch + 5s poll
│   │   └── warmpool_test.go       # 8 tests (mock runner client)
│   ├── scheduler/                 # cron schedule evaluator
│   │   ├── scheduler.go           # 30s ticker, robfig/cron/v3, catch-up-once policy
│   │   └── scheduler_test.go      # 8 tests (mock stores + executor)
│   ├── reaper/                    # data retention cleanup daemon
│   │   ├── reaper.go              # Background goroutine — prune runs, fail stuck, clean branches
│   │   ├── nessie_client.go       # Nessie v2 REST client for orphan branch cleanup
│   │   └── reaper_test.go         # 10 tests (mock stores)
│   ├── plugins/                   # plugin loader, auth middleware, gRPC clients
│   │   ├── loader.go              # Registry, Load(), Features(), health checks
│   │   ├── auth_middleware.go     # Bearer token → AuthService.Authenticate → context
│   │   ├── context.go             # ContextWithUser(), UserFromContext()
│   │   ├── loader_test.go         # 12 tests (mock ConnectRPC clients)
│   │   ├── auth_middleware_test.go # 7 tests
│   │   └── context_test.go        # 2 tests
│   ├── catalog/                   # Nessie REST client
│   ├── ownership/                 # ownership + sharing registry
│   ├── storage/                   # S3 operations (MinIO client)
│   └── domain/                    # shared domain types (Pipeline, Run, Schedule, Namespace, etc.)
├── go.mod
├── go.sum
└── Dockerfile
```

### Database (sqlc)

```sql
-- queries/pipelines.sql
-- name: GetPipeline :one
SELECT * FROM pipelines WHERE namespace = $1 AND layer = $2 AND name = $3;

-- name: CreatePipeline :one
INSERT INTO pipelines (namespace, layer, name, owner, s3_path)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
```

`sqlc` generates type-safe Go code from SQL. No ORM, no magic — just SQL.

---

## Python Guidelines (runner/, query/)

### Version & Tooling

- **Python 3.12+**
- **Package manager**: `uv` (fast, replaces pip + venv)
- **Build**: `pyproject.toml` (PEP 621)
- **DuckDB**: `duckdb` (latest)
- **Arrow**: `pyarrow`
- **Iceberg**: `pyiceberg`
- **gRPC**: `grpcio` + `grpcio-tools` (or ConnectRPC Python client)
- **Testing**: `pytest` + `pytest-cov`
- **Linting**: `ruff` (replaces black + isort + flake8)
- **Type checking**: `pyright` (strict mode)

### Code Style

```python
# DO: type hints everywhere
def execute_pipeline(spec: PipelineSpec, conn: duckdb.DuckDBPyConnection) -> RunResult:
    """Execute a pipeline and return the result."""
    ...

# DO: explicit error handling, no bare except
try:
    result = conn.execute(sql).fetch_arrow_table()
except duckdb.Error as e:
    raise PipelineExecutionError(f"SQL failed: {e}") from e

# DO: dataclasses or Pydantic for structured data
@dataclass(frozen=True)
class PipelineSpec:
    namespace: str
    layer: str
    name: str
    sql: str
    config: PipelineConfig

# DON'T: mutable global state
# DON'T: wildcard imports (from x import *)
# DON'T: Python loops for data manipulation — use DuckDB SQL or PyArrow
```

### Package Layout

```
runner/
├── src/rat_runner/
│   ├── __init__.py
│   ├── __main__.py                # Entrypoint (sys.path + serve)
│   ├── server.py                  # gRPC RunnerServiceImpl (4 RPCs) + cleanup daemon
│   ├── executor.py                # 5-phase pipeline execution (branch → write → test → merge)
│   ├── engine.py                  # DuckDB engine (S3 + Iceberg extensions)
│   ├── templating.py              # Jinja SQL — ref(), this, is_incremental(), watermark
│   ├── iceberg.py                 # PyIceberg writes (overwrite + merge) + watermark reads
│   ├── nessie.py                  # Nessie v2 REST client (branch lifecycle)
│   ├── python_exec.py             # Python pipeline execution via exec()
│   ├── quality.py                 # Quality test discovery + execution
│   ├── config.py                  # S3Config, NessieConfig, YAML parsing, S3 listing
│   ├── models.py                  # RunState, RunStatus, LogRecord, QualityTestResult
│   ├── log.py                     # RunLogger (deque + Python logging)
│   └── gen/                       # Generated gRPC stubs
├── tests/
│   ├── conftest.py
│   └── unit/                      # 140 tests, 91% coverage
│       ├── test_models.py
│       ├── test_config.py
│       ├── test_engine.py
│       ├── test_templating.py
│       ├── test_iceberg.py
│       ├── test_log.py
│       ├── test_executor.py
│       ├── test_server.py
│       ├── test_cleanup.py
│       ├── test_nessie.py
│       ├── test_python_exec.py
│       └── test_quality.py
├── pyproject.toml
└── Dockerfile
```

---

## TypeScript Guidelines (portal/, sdk-typescript/)

### Version & Tooling

- **Node 20+ / TypeScript 5+**
- **Framework**: Next.js 14+ (App Router)
- **UI**: shadcn/ui + Tailwind CSS
- **State**: SWR for data fetching
- **Editor**: CodeMirror 6
- **DAG**: Mermaid
- **Testing**: Vitest + Testing Library
- **Linting**: ESLint + Prettier (biome later)

### Code Style

```typescript
// DO: strict types, no `any`
interface Pipeline {
  namespace: string;
  layer: "bronze" | "silver" | "gold";
  name: string;
  owner: string | null;
}

// DO: server components by default, client only when needed
// "use client" only for interactivity (forms, state, effects)

// DO: error boundaries for graceful failures
// DO: SWR for all API data fetching (caching, revalidation)

// DON'T: inline styles (use Tailwind classes)
// DON'T: prop drilling more than 2 levels (use context or composition)
// DON'T: fetch in useEffect (use SWR hooks)
```

### UI Theme

Underground/squat-collective aesthetic from v1:
- Neon green + purple, no rounded corners (`--radius: 0px`)
- CSS classes: `rat-bg`, `brick-texture`, `brutal-card`, `neon-text`, `gradient-text`
- `useScreenGlitch()` hook for error feedback
- DataTable: zebra stripes, row numbers, type-colored values

---

## Proto Guidelines (proto/)

### Tooling

- **buf.build** for linting, breaking change detection, code generation
- **ConnectRPC** as the gRPC framework (Go + Python + TypeScript)

### Conventions

```protobuf
// proto/runner/v1/runner.proto
syntax = "proto3";
package ratatouille.runner.v1;

import "common/v1/common.proto";

// Services use verb-noun naming
// Shared types (GetRunStatus, StreamLogs, CancelRun) live in common/v1/common.proto
service RunnerService {
  rpc SubmitPipeline(SubmitPipelineRequest) returns (SubmitPipelineResponse);
  rpc GetRunStatus(ratatouille.common.v1.GetRunStatusRequest) returns (ratatouille.common.v1.GetRunStatusResponse);
  rpc StreamLogs(ratatouille.common.v1.StreamLogsRequest) returns (stream ratatouille.common.v1.LogEntry);
  rpc CancelRun(ratatouille.common.v1.CancelRunRequest) returns (ratatouille.common.v1.CancelRunResponse);
  rpc PreviewPipeline(PreviewPipelineRequest) returns (PreviewPipelineResponse);
  rpc ValidatePipeline(ValidatePipelineRequest) returns (ValidatePipelineResponse);
}

// Messages: Request/Response suffix, fields are snake_case
message SubmitPipelineRequest {
  string namespace = 1;
  ratatouille.common.v1.Layer layer = 2;
  string pipeline_name = 3;
  string trigger = 4;  // "manual", "schedule:hourly", "sensor:upstream"
}
```

### File Layout

```
proto/
├── buf.yaml                       # buf configuration
├── buf.gen.yaml                   # code generation config
├── common/v1/common.proto         # shared types (Layer, RunStatus, GetRunStatus, StreamLogs, CancelRun, LogEntry)
├── runner/v1/runner.proto         # runner service (6 RPCs: Submit, GetRunStatus, StreamLogs, CancelRun, Preview, Validate)
├── query/v1/query.proto           # query service (4 RPCs)
├── executor/v1/executor.proto     # executor plugin service (Submit, GetRunStatus, StreamLogs, Cancel)
├── plugin/v1/plugin.proto         # base PluginService — HealthCheck (every plugin implements)
├── auth/v1/auth.proto             # auth plugin — Authenticate, Authorize
├── cloud/v1/cloud.proto           # cloud plugin — GetCredentials
├── sharing/v1/sharing.proto       # sharing plugin — ShareResource, RevokeAccess, ListAccess, TransferOwnership
└── enforcement/v1/enforcement.proto  # enforcement plugin — CanAccess, GetCredentials
```

### Rules

- **Versioned packages** (`v1`, `v2`) — never break existing proto
- **Request/Response per RPC** — no shared request messages
- **Use `buf lint`** before every commit
- **`buf breaking`** in CI — detect backward-incompatible changes

---

## Make Is The Single Entry Point

**Every recurrent command goes through `make`.** No raw `docker run`, `go test`, `npm run`, or `pytest` commands. If you find yourself typing a raw command more than once, add it to the Makefile.

### Why

- **Standardization**: everyone runs the same command the same way
- **Documentation**: `make help` shows all available commands
- **Reproducibility**: Makefile IS the command reference — no tribal knowledge
- **Containerized**: all `make` targets run inside Docker — nothing on the host

### Rules

- **Always use `make <target>`** — never run raw build/test/lint commands directly
- **Group related operations** — `make test` runs all tests, not separate commands per file
- **Add new targets** when a new recurrent command emerges
- **Keep targets idempotent** — running twice produces the same result
- **Document every target** with `## comment` for `make help`

### Available Targets

```bash
make help             # show all targets
make up               # start all 7 services
make down             # stop everything
make build            # build all Docker images
make test             # run ALL tests (Go + Python + TS)
make test-go          # Go tests only
make test-py          # Python tests only (runner + query)
make test-ts          # TypeScript tests only (SDK + portal)
make lint             # lint all code (Go + Python + Proto)
make fmt              # format all code
make proto            # generate gRPC code from proto files
make sdk-build        # build TypeScript SDK (tsup → ESM + CJS + DTS)
make sdk-test         # build + test SDK (27 vitest tests)
make portal-build     # build portal for production (standalone Next.js)
make portal-typecheck # type-check portal without full build
make dev-ratd         # hot reload Go platform
make dev-portal       # hot reload Next.js portal (builds SDK first)
make clean            # remove containers, volumes, generated files
```

---

## Docker Guidelines

### Rules

- **Never install anything on the host machine** — everything runs in containers
- **Every service has its own Dockerfile** — no shared images (except base)
- **Multi-stage builds** — build stage + runtime stage (small final images)
- **Non-root users** in production images
- **Health checks** on every service

### Image Sizes (targets)

| Service | Base | Target Size |
|---------|------|-------------|
| ratd | `scratch` (static Go binary) | ~15-20 MB |
| runner | `python:3.12-slim` | ~200 MB |
| ratq | `python:3.12-slim` | ~200 MB |
| portal | `node:20-alpine` (standalone) | ~100 MB |

### Compose Conventions

```yaml
services:
  ratd:
    build: ./platform
    depends_on:
      postgres: { condition: service_healthy }
      minio: { condition: service_healthy }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/health"]
      interval: 5s
      retries: 3
```

---

## Development Workflow

### First Time Setup

```bash
cd ~/sandbox/ratatouille-v2/ratatouille
make setup        # pull base images, generate proto, install deps
make up           # start all 7 services
```

### Daily Development

```bash
make up               # start services
make dev-ratd         # hot reload Go platform (air)
make dev-portal       # hot reload Next.js portal (builds SDK first)
make sdk-build        # build TypeScript SDK
make sdk-test         # test SDK (27 vitest tests)
make portal-build     # production portal build
make portal-typecheck # type-check portal only
make test             # run all tests
make test-go          # Go tests only
make test-py          # Python tests only
make test-ts          # TypeScript tests only (SDK + portal)
make proto            # regenerate from proto files
make lint             # lint all languages
make down             # stop everything
```

### Adding a New Feature (TDD flow)

```bash
# 1. Create branch
git checkout -b feat/ratd-pipeline-crud

# 2. Write failing test
# Edit platform/internal/api/pipelines_test.go

# 3. Run test — see it fail (RED)
make test-go

# 4. Write implementation
# Edit platform/internal/api/pipelines.go

# 5. Run test — see it pass (GREEN)
make test-go

# 6. Refactor if needed, tests stay green
make test-go && make lint

# 7. Commit and push
git add -A && git commit -m "feat(ratd): add pipeline CRUD endpoints"
git push -u origin feat/ratd-pipeline-crud

# 8. Open PR → review → squash merge to main
```

---

## Documentation — Everything Is Documented

**Code without documentation is unfinished code.** Every change must be documented at the right level. If it's not documented, it doesn't exist.

### The Rule

> When you change code, update the docs. When you change architecture, update the strategy. When you add a feature, document it. No exceptions.

### What to Document and Where

| Change | Document Where |
|--------|---------------|
| New function/method | **Inline** — docstring/godoc explaining _why_, not _what_ |
| New API endpoint | **Proto file** — comments on service/rpc + `docs/api.md` if public |
| New config option | **`rat.yaml` example** + inline comment |
| Architecture change | **`docs/v2-strategy.md`** — update the relevant section |
| Significant design decision | **ADR** in `docs/adr/` (see below) |
| New service / component | **CLAUDE.md** — update Architecture section + repo structure |
| Proto message change | **Proto file** — comments on every field |
| Environment variable | **`docker-compose.yml`** comments + `docs/config.md` |
| Bug fix with non-obvious cause | **Code comment** explaining the _why_ behind the fix |
| Breaking change | **ADR** + migration note in `docs/migrations/` |

### Code Documentation

```go
// Go — godoc style. Focus on WHY and WHEN, not WHAT.
// SubmitPipeline dispatches a pipeline run to the warm pool executor.
// It validates the spec, resolves ref() dependencies, and returns a run handle.
// The caller should use StreamLogs to follow execution progress.
func (s *ExecutorService) SubmitPipeline(ctx context.Context, req *SubmitPipelineRequest) (*RunHandle, error) {
```

```python
# Python — docstrings on public functions. Explain behavior, not implementation.
def resolve_refs(sql: str, namespace: str) -> str:
    """Replace ref("table") calls with fully qualified Iceberg table paths.

    Handles cross-namespace refs like ref("other_ns.silver.orders").
    Raises RefResolutionError if a referenced table doesn't exist.
    """
```

```typescript
// TypeScript — JSDoc on exported functions and complex hooks.
/**
 * Fetches pipeline metadata and auto-refreshes after runs complete.
 * Returns null during loading, error state on failure.
 */
export function usePipelineMeta(namespace: string, layer: string, name: string) {
```

### Documentation Files

```
docs/
├── v2-strategy.md              # 🎯 THE source of truth for architecture decisions
├── api-spec.md                 # REST API reference (75 endpoints)
├── config.md                   # All configuration options + environment variables
├── postgres-schema.sql         # Postgres schema (14 tables)
├── adr/                        # Architecture Decision Records
│   ├── 001-s3-storage.md       # S3Store via MinIO Go SDK
│   ├── 002-auth-middleware.md  # No-op auth with plugin slot
│   ├── 003-warmpool-executor.md # WarmPoolExecutor + ConnectRPC dispatch
│   ├── 004-scheduler.md        # Background cron scheduler
│   ├── 005-runner-service.md   # Runner service architecture
│   ├── 006-query-service.md    # Query service (ratq) architecture
│   ├── 007-plugin-system.md    # Plugin system foundation (v2.4)
│   ├── 008-auth-keycloak.md    # Auth-Keycloak — first Pro plugin (v2.5)
│   ├── 009-container-executor.md  # ContainerExecutor Pro plugin (v2.6)
│   ├── 010-acl-plugin.md       # ACL sharing + enforcement plugin (v2.7)
│   ├── 011-cloud-aws.md        # Cloud AWS plugin (v2.8)
│   ├── 012-license-gating.md   # License gating for Pro plugins (v2.9)
│   └── ...
└── migrations/                 # Breaking change migration guides
```

### Staleness Prevention

- **Strategy doc** (`v2-strategy.md`): reviewed and updated with every architectural PR
- **CLAUDE.md**: updated whenever tooling, conventions, or structure change
- **ADRs**: immutable once accepted (superseded by new ADRs, never edited)
- **Code comments**: updated in the same PR as the code change — stale comments are worse than no comments

---

## PR Checklist

Every PR must satisfy:

- [ ] Tests written FIRST (TDD — test existed before implementation)
- [ ] All tests passing (`make test`)
- [ ] Linting clean (`make lint`)
- [ ] No `any` types in TypeScript
- [ ] No `# type: ignore` in Python (fix the type error)
- [ ] Proto changes are backward-compatible (`buf breaking`)
- [ ] Docker images build successfully (`make build`)
- [ ] PR description explains **why**, not just **what**
- [ ] No secrets, credentials, or `.env` files committed
- [ ] **Docs updated** — code comments, proto comments, strategy doc, CLAUDE.md as needed
- [ ] **ADR written** if the PR introduces a significant design decision

---

## Architecture Decision Records (ADRs)

For significant design decisions, write an ADR in `docs/adr/`:

```markdown
# ADR-001: Use ConnectRPC for service communication

## Status: Accepted

## Context
We need gRPC communication between ratd, ratq, runner, and plugins...

## Decision
Use ConnectRPC instead of raw gRPC...

## Consequences
- Positive: HTTP/1.1 compatible, curl-friendly, simpler debugging
- Negative: Smaller ecosystem than raw gRPC
```

---

## Security

- **No secrets in code** — use environment variables
- **Validate all input** at API boundaries (ratd handlers)
- **Parameterized SQL** — never string-interpolate SQL (use sqlc for Go, parameterized DuckDB for Python)
- **CORS**: restrictive in production, open in dev
- **Container security**: non-root, read-only filesystem where possible
- **Dependency scanning**: Dependabot for Go/Python/TS

---

## Reference

- **v2 Strategy**: `docs/v2-strategy.md`
- **Config Reference**: `docs/config.md`
- **API Spec**: `docs/api-spec.md`
- **v1 Codebase** (reference): `~/sandbox/ratatouille/`
- **Pro Plugins** (private): `~/sandbox/ratatouille-v2/ratatouille-pro/`
- **Pro Compose**: `docker compose -f ratatouille/infra/docker-compose.yml -f ratatouille-pro/infra/docker-compose.pro.yml up`
