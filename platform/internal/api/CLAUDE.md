# ratd HTTP API — non-obvious wiring

(Go conventions are in `.claude/rules/go.md`; this is just the orientation that isn't obvious from reading a single handler.)

## Two routers, two trust levels
ratd serves **two** listeners — get this wrong and you create a security hole or a 404:

- **Public** (`NewRouter`, `:8080`) — all end-user `/api/v1/*`. Goes through auth middleware. This is where normal handlers live.
- **Internal** (`NewInternalRouter` → `MountAllInternalRoutes`, `:8090`) — service-to-service callbacks: runner run-status, plugin phone-home, failed-merge audit. **No auth** — trust is network isolation (ADR-019). `internal_routes.go` is the *single* place these mount; add new internal endpoints there, never scatter them.

A handler that looks ordinary but is mounted on the internal router is **deliberately auth-less**. Don't move internal endpoints to the public router (they'd suddenly require auth and break the runner), and don't add user-facing endpoints to the internal one (they'd be unauthenticated).

## Enforcement on reads (Pro)
Read handlers (`HandleList*`, `HandleGet*`) post-filter through `s.filterAccess(...)` / `requireAccess(...)`. In CE the NoopAuthorizer passes everything; in Pro the enforcement plugin filters. When you add a list/get endpoint, wire the filter — a read path that skips it leaks across the ACL boundary (the bug Wave 8 fixed).

## Version
`api.Version`/`GitCommit`/`BuildTime` are ldflags-injected at release build — `health.go` defaults to `"dev"`, never hardcode a real version.
