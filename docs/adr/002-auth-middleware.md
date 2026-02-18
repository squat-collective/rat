# ADR-002: No-Op Auth Middleware with Plugin Slot

## Status: Accepted

## Context

ratd needs an auth layer for the `/api/v1` route group. Community edition is single-user with
no authentication required. Pro edition will plug in real auth middleware (Keycloak OIDC, Cognito,
etc.) via the plugin system.

Key requirements:
- Community: zero-friction, no login, no config — `docker compose up` and go
- Pro: swappable auth via plugin container (gRPC auth plugin)
- Auth middleware must be hot-swappable without changing handler code
- Health endpoint (`GET /health`) must remain unauthenticated

## Decision

### Plugin slot pattern

Auth is a `func(http.Handler) http.Handler` field on the `Server` struct:

```go
type Server struct {
    // ...
    Auth func(http.Handler) http.Handler
}
```

Community ships with `auth.Noop()` which passes every request through unchanged:

```go
func Noop() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler { return next }
}
```

The middleware is applied conditionally in the router:

```go
r.Route("/api/v1", func(r chi.Router) {
    if srv.Auth != nil {
        r.Use(srv.Auth)
    }
    // ... mount handlers
})
```

### Why `func(http.Handler) http.Handler` and not an interface?

- **Standard Go idiom**: chi middleware is exactly this signature
- **Zero abstraction tax**: no interface to satisfy, no struct to allocate
- **Composable**: Pro plugins can chain multiple middlewares (OIDC + RBAC) into one
- **No import cycle**: the auth package imports nothing from api/

### Why not skip the middleware entirely for Community?

Even though Noop does nothing, wiring it explicitly:
1. Proves the plugin slot works (tested in CI)
2. Documents the extension point for plugin authors
3. Makes the Server struct self-documenting (`Auth` field is always set)

## Consequences

### Positive
- Zero overhead in Community — Noop middleware compiles to a direct handler call
- Pro plugins just provide a different `func(http.Handler) http.Handler`
- Health endpoint is outside `/api/v1`, always unauthenticated
- 3 unit tests validate the middleware contract

### Negative
- No built-in user context extraction — Pro auth plugin must set context values
- No built-in CORS handling — will need a separate middleware when portal is on a different origin

## Implementation

- `platform/internal/auth/middleware.go` — `Noop()` function
- `platform/internal/auth/middleware_test.go` — 3 tests
- `platform/internal/api/router.go` — `Auth` field on Server, conditional `r.Use()`
- `platform/cmd/ratd/main.go` — `srv.Auth = auth.Noop()`
