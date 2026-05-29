---
paths:
  - "platform/**/*.go"
---

# Go rules (ratd platform)

**Toolchain:** Go 1.26 · chi (router) · ConnectRPC + `google.golang.org/protobuf` · `robfig/cron/v3` · MinIO Go SDK · `pgx` + `sqlc` · `slog` · stdlib `testing` + `testify`.

## Style
- Short, focused functions; errors are values — handle explicitly and wrap with `fmt.Errorf("...: %w", err)`.
- Use `context.Context` for cancellation/timeouts; it's the **first** parameter (revive `context-as-argument` is enforced).
- No naked returns, no `init()`, no global mutable state, no `panic` in library code (only `main` for truly unrecoverable).
- Define interfaces where they're **consumed**, not produced.
- Don't shadow builtins as identifiers — use `capName`/`next`, not `cap`/`new` (revive `redefines-builtin-id` is on).

## Package layout (`platform/internal/`)
`api/` (chi handlers, one file per resource) · `auth/` · `config/` · `executor/` · `scheduler/` · `reaper/` · `plugins/` · `catalog/` · `ownership/` · `storage/` · `domain/` (shared types). `cmd/ratd/main.go` is wiring only.

## Database
`sqlc` generates type-safe Go from `queries/*.sql` — no ORM. Never string-interpolate SQL.

## Linting (pinned — this is load-bearing)
- `make lint-go-strict` runs **golangci-lint v2.12.2** (pinned in `GO_LINT_IMAGE` and `ci.yml`; config is `platform/.golangci.yml`, **schema v2**). `make lint` runs `go vet`.
- `misspell` is **disabled** in `.golangci.yml` — the codebase mixes British (`behaviour`, `serialise`) and American (`initialize`) spelling; enforcing either is churn for no correctness gain.
- v2 enables the staticcheck **`QF*`** quickfix checks by default (De Morgan's law, tagged switch, embedded-field selectors) — `golangci-lint run --fix` auto-applies them.
- `govet` `nilness` flags defensive `if x != nil` guards as tautological when the var is provably assigned — if it's genuinely always-set, drop the guard; if not, the guard stays (don't blindly "fix" it).
- Version metadata (`api.Version`) is injected via ldflags at release build — never hardcode a real version in `health.go` (default stays `"dev"`).

## Before pushing Go changes
`make ci-quick` (lint + golangci + `go test -race`). The `git push` hook runs it automatically.
