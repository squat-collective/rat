# ADR-007: Plugin System Foundation

## Status: Accepted

## Context

RAT v2 has **implicit plugin slots** — the `Server` struct in `api/router.go` has fields
for Auth, Executor, Query, and Storage, but there's no plugin infrastructure. Everything is
hardcoded in `main.go` via env vars, and features are hardcoded in `health.go`.

We need a foundation for Pro edition plugins: auth (Keycloak OIDC), sharing, enforcement,
and eventually custom executors. Plugins are separate containers that communicate with
`ratd` via ConnectRPC (gRPC-compatible, HTTP/1.1 friendly — matching our existing patterns
for runner and query services).

Key questions resolved:
1. How does ratd discover and connect to plugins?
2. How does ratd know which features are active?
3. How does auth delegation work?
4. What happens when a plugin is unhealthy?

## Decision

### Config-driven plugin loading via `rat.yaml`

```yaml
edition: pro
plugins:
  auth:
    addr: "auth:50060"
    config:
      issuer: "https://keycloak.example.com"
  sharing:
    addr: "sharing:50061"
  enforcement:
    addr: "enforcement:50062"
```

**Resolution order**: `RAT_CONFIG` env var > `./rat.yaml` > no file (community defaults).

### ratd connects, never starts containers

Plugin containers are managed by Docker Compose / K8s — not by ratd.
ratd connects to already-running gRPC endpoints. This keeps ratd simple:
no container runtime dependency, no lifecycle management complexity.

### Custom `PluginService.HealthCheck` RPC

Every plugin container must implement `PluginService.HealthCheck` (defined in
`proto/plugin/v1/plugin.proto`). ratd calls it on startup with a 5-second timeout.

- **Healthy** (`STATUS_SERVING`) → plugin enabled, client created
- **Unhealthy** (error or `STATUS_NOT_SERVING`) → log warning, plugin disabled, community defaults
- **Unknown plugin name** → log warning, skip

This gives graceful degradation: if a plugin container crashes or isn't started,
ratd continues running with community defaults.

### Auth middleware delegation

When the auth plugin is loaded:
1. Middleware extracts `Bearer` token from `Authorization` header
2. Calls `AuthService.Authenticate(token)` via ConnectRPC
3. On success: stores `UserIdentity` in request context
4. On failure: returns 401

When no auth plugin: falls back to `auth.Noop()` (pass-through), identical to
the previous behavior.

### Dynamic features via `PluginRegistry`

`GET /api/v1/features` now delegates to `Registry.Features()` which reports
actual plugin status instead of hardcoded values. The portal uses this to
show/hide UI elements.

## Consequences

### Positive
- **Zero-config community** — no `rat.yaml` needed, everything works as before
- **Graceful degradation** — unhealthy plugins are disabled, not fatal
- **Clean separation** — ratd doesn't know about plugin internals, just gRPC contracts
- **Testable** — mock clients follow existing patterns (`warmpool_test.go`)
- **Extensible** — new plugin types just need a proto definition and a `case` in the loader

### Negative
- **No plugin auto-start** — requires external orchestration (compose/K8s)
- **No plugin hot-reload** — ratd must restart to pick up new plugins
- **Health check on startup only** — runtime plugin failures aren't detected until the next request fails

### New packages
- `platform/internal/config/` — YAML config loading (10 tests)
- `platform/internal/plugins/` — plugin loader, auth middleware, context (21 tests)

### New protos
- `proto/plugin/v1/plugin.proto` — base PluginService with HealthCheck
- `proto/auth/v1/auth.proto` — AuthService (Authenticate, Authorize)
- `proto/sharing/v1/sharing.proto` — SharingService (ShareResource, RevokeAccess, ListAccess, TransferOwnership)
- `proto/enforcement/v1/enforcement.proto` — EnforcementService (CanAccess, GetCredentials)
