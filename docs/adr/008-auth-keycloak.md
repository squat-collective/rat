# ADR-008: Auth-Keycloak — First Pro Plugin

## Status: Accepted

## Context

ADR-007 built the plugin infrastructure (config, loader, health checks, auth middleware,
proto definitions). Now we need the **first real Pro plugin** — a container that validates
Keycloak JWTs and returns user identity to ratd via ConnectRPC.

This plugin establishes the pattern for all future Pro plugins:
- Separate Go binary in `rat-pro/`
- Implements `PluginService.HealthCheck` + one domain service
- Configured via env vars, discovered by ratd via `rat.yaml`
- Docker Compose overlay on top of community stack

Key questions resolved:
1. How does the plugin validate tokens without calling Keycloak on every request?
2. How does it handle Keycloak not being ready at startup?
3. What claims map to `UserIdentity` fields?
4. How does the Pro compose overlay work with the community stack?

## Decision

### Go container with JWKS caching

The plugin is a standalone Go binary (~26MB Docker image) that:
1. Fetches OIDC discovery from Keycloak (`/.well-known/openid-configuration`)
2. Downloads JWKS (JSON Web Key Set) and caches RSA public keys in memory
3. Validates JWTs locally (no Keycloak call per request) — RS256 signature, `exp`, `iss`
4. Extracts claims into `UserIdentity` protobuf

**Rate-limited JWKS refresh**: max 1 refresh per 60 seconds. Prevents abuse from
a flood of unknown `kid` values while still picking up key rotations.

### Background JWKS prefetch with retry

Keycloak takes 30-60s to start. The plugin starts immediately and retries JWKS
prefetch in a background goroutine (up to 30 attempts, 10s interval).

- Before prefetch succeeds: `HealthCheck` returns `STATUS_NOT_SERVING`
- After prefetch succeeds: `HealthCheck` returns `STATUS_SERVING`
- ratd's plugin loader health-checks the plugin — it won't enable auth until the
  plugin reports healthy

This means the whole stack can start simultaneously without ordering concerns
beyond Docker Compose `depends_on`.

### Claim mapping

| JWT Claim | UserIdentity Field | Notes |
|-----------|-------------------|-------|
| `sub` | `user_id` | Keycloak unique user ID (UUID) |
| `email` | `email` | — |
| `name` | `display_name` | Fallback: `preferred_username`, then `email` |
| `policy` | `roles` | Keycloak group membership mapper (string array) |

The `policy` claim is a **custom Keycloak protocol mapper** (`oidc-group-membership-mapper`)
configured in the realm JSON. It maps the user's group paths (e.g., `/admin`, `/demo:analyst`)
to a string array claim. This is the same pattern used in v1.

### Authorize stub

`AuthService.Authorize` returns `CodeUnimplemented` via the embedded
`UnimplementedAuthServiceHandler`. Authorization will be implemented in v2.7
alongside the ownership + sharing plugin.

### Docker Compose overlay

Pro edition uses a compose overlay pattern:

```bash
docker compose \
  -f rat/infra/docker-compose.yml \
  -f rat-pro/infra/docker-compose.pro.yml \
  up -d
```

The overlay:
- Overrides `ratd` with `RAT_CONFIG` env var pointing to Pro config
- Adds `keycloak` service (port 8180, realm auto-import)
- Adds `auth-keycloak` service (port 50060, depends on keycloak healthy)

Community users run `docker compose up` as before — no change.

### Keycloak realm (adapted from v1)

The v1 `rat-realm.json` was adapted:
- **Removed** `minio` client (v2 doesn't use MinIO OIDC)
- **Removed** `audience` mapper from all clients (was for MinIO STS)
- **Updated** `rat-ui` redirect URIs to port 3000 (v2 portal)
- **Kept** `policy` group mapper, demo users, groups

## Consequences

### Positive
- **No per-request Keycloak call** — JWT validation is local (sub-millisecond)
- **Graceful startup** — plugin retries JWKS until Keycloak is ready
- **Key rotation support** — unknown `kid` triggers JWKS refresh
- **OIDC-standard** — any OIDC provider can replace Keycloak (just change env vars)
- **Pattern established** — future plugins follow the same structure
- **17 unit tests** — config (3), JWKS (5), auth handler (7), plugin handler (2)

### Negative
- **No token revocation check** — JWT is validated locally, no introspection endpoint call. Revoked tokens remain valid until `exp`. Acceptable for v2.5.
- **No key rotation notification** — relies on cache miss + rate-limited refresh. Key rotation takes up to 60s to propagate in worst case.
- **Keycloak-specific realm JSON** — switching OIDC providers requires a new realm config. But the plugin code itself is generic OIDC.

### File layout

```
rat-pro/plugins/auth-keycloak/
├── cmd/auth-keycloak/main.go           # Entrypoint
├── internal/config/config.go           # Env var parsing
├── internal/jwks/jwks.go               # JWKS cache + OIDC discovery
├── internal/handler/auth.go            # AuthServiceHandler
├── internal/handler/plugin.go          # PluginServiceHandler
├── go.mod                              # replace directive → platform/gen
├── Dockerfile                          # Multi-stage (~26MB)
└── Makefile                            # Docker-only targets
```

### Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `KEYCLOAK_URL` | Yes | — | Keycloak base URL (e.g., `http://keycloak:8180`) |
| `KEYCLOAK_REALM` | Yes | — | Realm name (e.g., `rat`) |
| `GRPC_PORT` | No | `50060` | ConnectRPC listen port |
| `ISSUER_URL` | No | `{KEYCLOAK_URL}/realms/{REALM}` | Override issuer URL for JWT validation |
