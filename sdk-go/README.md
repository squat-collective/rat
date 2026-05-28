# `sdk-go` — shared helpers for RAT example plugins

> Internal helper library for the Go plugins under `plugins/rat-plugin-*`.
> Not published or versioned — vendored via a local `replace` directive.

## What it is

A small Go module that captures the ~150 LOC of boilerplate every example
plugin grew across waves 1-4: per-startup token generation, SRI hashing,
the `X-RAT-Plugin-Token` middleware, the phone-home retry loop, env-var
fan-out, the standard mux wiring, and a fluent `DescribeResponse` builder.

## Why

Before this SDK, any change to the platform-token contract or the
phone-home payload meant editing 14 plugin copies in lockstep. Extracting
the helpers means a contract change touches one place plus thin shims.

## How to use it

In your plugin's `go.mod`:

```go
require github.com/rat-data/rat/sdk-go v0.0.0

replace github.com/rat-data/rat/sdk-go => ../../sdk-go
```

In your plugin's `Dockerfile`:

```dockerfile
COPY --from=sdk . /sdk-go/
```

And in your `Makefile` build target:

```makefile
docker build --build-context platform=platform --build-context sdk=sdk-go ...
```

Then in `main.go`:

```go
import sdk "github.com/rat-data/rat/sdk-go"

env := sdk.LoadPluginEnv("myplugin", "50099", "myplugin:50099")
token := sdk.RandomToken()
hash := sdk.SRIHash(bundleJS)
handler := sdk.MountStandardPluginRoutes(mux, pluginHandler, bundleJS, token, restMux)
go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
```

## Public API

- `RandomToken() string` — fresh per-startup platform token.
- `SRIHash(b []byte) string` — `"sha256-<base64>"` for the embedded bundle.
- `TokenAuth(expected string, next http.Handler) http.Handler` — middleware
  with `/health` and `/bundle.js` allowlisted.
- `PhoneHome(ctx, internalURL, name, addr, maxAttempts) error` / `PhoneHomeLoop(...)`.
- `PhoneHomeWithOptions(ctx, internalURL, name, addr, opts) error` / `PhoneHomeLoopWithOptions(...)`.

### Phone-home retry schedule

`PhoneHomeLoop` calls `PhoneHomeWithOptions` with `DefaultPhoneHomeOptions()`:

| Option           | Default | Meaning                                  |
|------------------|---------|------------------------------------------|
| `MaxAttempts`    | `10`    | total register attempts including the first |
| `InitialBackoff` | `1s`    | pause before attempt #2 (attempt #1 fires immediately) |
| `MaxBackoff`     | `30s`   | cap for the exponential growth           |

That produces this curve (wait before each attempt):

```
attempt 1 → 0s     (immediate)
attempt 2 → 1s
attempt 3 → 2s
attempt 4 → 4s
attempt 5 → 8s
attempt 6 → 16s
attempt 7..10 → 30s each (capped)
total wall-clock ≈ 3 minutes for 10 attempts
```

The slow ramp + cap is deliberate: a crashlooping or compromised plugin
cannot hammer ratd's internal listener as fast as the old fixed-2s
schedule allowed. Previously: 30 attempts × 2s = constant 60s of pressure.
Now: at most one register every ~30s once the curve flattens.

Use `PhoneHomeLoopWithOptions(url, name, addr, opts)` if you need a
tighter or looser schedule (CI smoke tests, slow-boot plugins, etc).
- `LoadPluginEnv(defaultName, defaultPort, defaultAddr) PluginEnv`.
- `MountStandardPluginRoutes(mux, pluginHandler, bundleJS, token, restMux) http.Handler`.
- `H2CHandler(h http.Handler) http.Handler` — h2c wrapper.
- `NewDescribe(...).WithRoute(...).WithUI(...).WithPlatformToken(...).Build()`.

## Tests

```bash
docker run --rm \
  -v "$(pwd)/sdk-go":/work \
  -v "$(pwd)/platform":/platform \
  -w /work golang:1.24-alpine \
  sh -c "go mod tidy && go test ./..."
```
