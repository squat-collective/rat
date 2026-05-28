# RAT Plugin Author Guide

> Build a RAT plugin in one afternoon. Battle-tested patterns extracted from
> the 14 plugins living under `plugins/rat-plugin-*`.

---

## What is a RAT plugin?

A RAT plugin is a separate container that extends the platform across three
layers: a **runner-side** layer (optional — Python entry points the runner
discovers via `pip install`), a **platform-side gRPC** layer (a Go service
that implements `proto/plugin/v1/PluginService` and is dialed by `ratd` over
ConnectRPC), and a **portal UI bundle** (an optional `bundle.js` that the
portal injects into the browser via a `<script>` tag). Most plugins use one
or two of the three. The canonical Go pattern — what this guide documents —
lives in [`sdk-go`](../sdk-go/) and is used by every example plugin since
Wave 5.

---

## Quickstart

A minimal Go plugin needs five files. Copy the layout from
[`plugins/rat-plugin-secrets/`](../plugins/rat-plugin-secrets/) — it's the
smallest reference plugin in the tree.

```
my-plugin/
├── main.go         # boot: env, token, mux, phone-home
├── handler.go      # implements PluginService (HealthCheck, Describe)
├── bundle.js       # portal UI bundle (optional; embed with go:embed)
├── Dockerfile      # multi-stage build with shared build contexts
├── Makefile        # docker build / run / test
└── go.mod          # replace github.com/rat-data/rat/sdk-go => ../../sdk-go
```

`main.go`:

```go
//go:embed bundle.js
var bundleJS []byte
var bundleHash = sdk.SRIHash(bundleJS)

func main() {
    env := sdk.LoadPluginEnv("myplugin", "50099", "myplugin:50099")
    token := sdk.RandomToken()
    h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, token)
    mux := http.NewServeMux()
    handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, token, restMux)
    go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
    server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
    server.ListenAndServe()
}
```

`handler.go` implements `HealthCheck` + `Describe` (10 lines each — use
`sdk.NewDescribe(...)` for the latter).

`Dockerfile` builds with two build contexts (see "Building" below).

`Makefile` wraps `docker build` so contributors don't have to remember the
build-context flags.

That's it. `make build && make run` and the plugin appears in `ratd`'s
registry within ~2 seconds of startup.

---

## The plugin contract

Five concepts every plugin author MUST internalise:

- **`Describe()`** — ratd calls this immediately after phone-home. The
  response declares the plugin's name, version, capabilities, HTTP routes,
  UI bundle, config schema, and **platform_token**. ratd caches this; a
  plugin restart must re-phone-home so ratd re-fetches.
- **`HealthCheck()`** — ratd polls this every few seconds. Return
  `STATUS_SERVING` once you're ready to accept traffic. Return
  `STATUS_NOT_SERVING` to make ratd disable the plugin (e.g., license
  check failed). `/health` is always reachable without `X-RAT-Plugin-Token`.
- **Phone-home** — on startup, POST `{"name":..., "addr":...}` to
  `${RATD_INTERNAL_URL}/internal/plugins/register`. ratd then dials the
  plugin's address to call `Describe()`. The internal listener is on a
  separate port from the public API; see [ADR-019](adr/019-internal-listener-split.md).
- **`platform_token`** — a per-startup random hex string the plugin
  generates at boot and advertises in `Describe()`. ratd injects it as
  `X-RAT-Plugin-Token` on every proxied request. The plugin's middleware
  rejects requests without the matching token. This prevents a compromised
  peer container on the docker network from calling your plugin directly.
  See [ADR-020](adr/020-platform-token.md).
- **`bundle_hash` (SRI)** — `"sha256-<base64>"` over the embedded
  `bundle.js`. The portal renders `<script integrity="sha256-…">`, so a
  tampered bundle delivered through the proxy is rejected by the browser.
  `sdk.SRIHash()` computes it.

---

## Anatomy of `sdk-go`

Seven public symbols, each replacing ~20 LOC of identical boilerplate
across plugins. Source: [`sdk-go/`](../sdk-go/).

- `RandomToken() string` — 32 bytes from `crypto/rand`, hex-encoded.
  Generated once in `main`; passed to both `MountStandardPluginRoutes` and
  the `Describe` builder. Panics if entropy is unavailable.
- `SRIHash(b []byte) string` — SHA-256 → `"sha256-<base64>"`. Use over a
  `go:embed`'d bundle to populate the `bundle_hash` field in `Describe()`.
- `TokenAuth(expected, next)` — HTTP middleware that requires
  `X-RAT-Plugin-Token == expected`. Constant-time compare (see
  [ADR-020](adr/020-platform-token.md)). Auto-allowlists `/health` and
  `/bundle.js`. Passing `expected == ""` is a no-op for backward compat.
- `PhoneHome(ctx, url, name, addr, maxAttempts)` — single retry loop.
  `PhoneHomeLoop(url, name, addr)` is the boot-time convenience: 30
  attempts, log on success, `os.Exit(1)` on terminal failure (a plugin
  ratd can't see is useless).
- `LoadPluginEnv(name, port, addr)` — reads `PLUGIN_NAME`, `PLUGIN_ADDR`,
  `GRPC_PORT`, `RATD_URL`, `RATD_INTERNAL_URL` with sensible defaults.
  Returns a `PluginEnv` struct.
- `MountStandardPluginRoutes(mux, pluginHandler, bundleJS, token, restMux)`
  — mounts the ConnectRPC plugin service on the bare mux (unauthenticated
  — that's how ratd learns the token), mounts `/bundle.js` on the bare mux
  (browsers can't add headers to `<script>` tags), then wraps `restMux`
  with `TokenAuth` and mounts it at `/`. A nil `restMux` becomes a
  TokenAuth-protected 404; an empty bundle skips the bundle mount.
- `H2CHandler(h)` — `h2c.NewHandler(h, &http2.Server{})`. ConnectRPC over
  cleartext HTTP/2.
- `NewDescribe(...).WithRoute(...).WithUI(...).WithPlatformToken(...).Build()`
  — fluent builder for `DescribeResponse`. Surfaces every proto field
  without exposing the proto types in your `handler.go`.

---

## Building + running

The Dockerfile uses Docker's [named build contexts](https://docs.docker.com/build/building/context/#named-contexts)
to copy `platform/` (for the generated proto) and `sdk-go/` (for the
helpers) into the build:

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /plugin .
```

The `go.mod` has matching `replace` directives:

```go
replace github.com/rat-data/rat/platform => /platform
replace github.com/rat-data/rat/sdk-go => /sdk-go
```

Build from the repo root (the `Makefile` wraps this):

```bash
docker build \
  --build-context platform=platform \
  --build-context sdk=sdk-go \
  -f plugins/rat-plugin-myplugin/Dockerfile \
  -t rat-plugin-myplugin \
  plugins/rat-plugin-myplugin
```

Run with:

```bash
docker run -d --name myplugin --network infra_default \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  rat-plugin-myplugin
```

`RATD_INTERNAL_URL` points at ratd's private listener (port 8090). See
[ADR-019](adr/019-internal-listener-split.md).

---

## Capabilities

ratd recognises five well-known capability strings in
`DescribeResponse.capabilities`:

- `auth` — plugin implements `Authenticate(token)`. ratd middleware
  delegates to it. See [ADR-008](adr/008-auth-keycloak.md).
- `executor` — plugin implements `ExecutorService` (a separate proto).
  Replaces the warm-pool executor. See [ADR-009](adr/009-container-executor.md).
- `cloud` — plugin implements `CloudService.GetCredentials`. Vends scoped
  STS/federated credentials per `(user, namespace)`. See [ADR-018](adr/018-cloud-credential-vending.md).
- `sharing` — plugin implements `SharingService` (grant/revoke/list).
- `enforcement` — plugin implements `Authorize(user, resource, action)`.

For anything else — custom integration surfaces, AI providers,
notification fan-out — register a **named capability** with the
`interconnect` plugin at runtime:

```go
POST /api/v1/x/interconnect/register
{
  "name": "secrets.get",
  "provider": "secrets",
  "method": "POST",
  "path": "/resolve",
  "description": "Resolve a secret by name."
}
```

Other plugins discover and call it through the broker. See
[`plugins/rat-plugin-secrets/main.go`](../plugins/rat-plugin-secrets/main.go)
for the registration pattern (loop forever so an interconnect restart
doesn't silently drop the capability).

---

## Cross-references

- [ADR-007](adr/007-plugin-system.md) — plugin system foundation
- [ADR-017](adr/017-python-pipeline-trust-model.md) — operator-trust model
- [ADR-018](adr/018-cloud-credential-vending.md) — cloud-credential vending
- [ADR-019](adr/019-internal-listener-split.md) — internal listener split
- [ADR-020](adr/020-platform-token.md) — platform_token contract
- [ADR-021](adr/021-sdk-go-extraction.md) — `sdk-go` extraction rationale

Reference plugins by topic:

- **Encryption / secret storage** → [`rat-plugin-secrets`](../plugins/rat-plugin-secrets/)
- **SQL data pipelines** → [`rat-plugin-pg-sync`](../plugins/rat-plugin-pg-sync/)
- **Time-travel data viewers** → [`rat-plugin-diff`](../plugins/rat-plugin-diff/)
- **Capability registry + broker** → [`rat-plugin-interconnect`](../plugins/rat-plugin-interconnect/)
- **Configurable AI provider** → [`rat-plugin-ai-provider`](../plugins/rat-plugin-ai-provider/)
