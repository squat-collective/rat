# ADR-021: SDK extraction (`sdk-go`)

## Status: Accepted (2026-05-27)

## Context

By the end of Wave 4 every example plugin under `plugins/rat-plugin-*`
had grown the same ~150 LOC of boilerplate: per-startup token, SRI
hashing, `X-RAT-Plugin-Token` middleware, phone-home retry loop,
env-var fan-out, mux wiring, and a verbose `DescribeResponse`
construction. Fourteen identical copies.

That duplication was real tax. The Wave 7 constant-time-compare fix
(`1d0dd04`) was a one-character bug that would have required editing
14 plugins individually; the platform-token rollout in Wave 2 landed
in 3 plugins and a follow-up had to repeat the same change in 11 more.

## Decision

Extract the shared helpers into `sdk-go/`, a small Go module vendored
via `replace github.com/rat-data/rat/sdk-go => ../../sdk-go` in every
plugin's `go.mod`. Public API: `RandomToken`, `SRIHash`, `TokenAuth`,
`PhoneHome`/`PhoneHomeLoop`, `LoadPluginEnv`,
`MountStandardPluginRoutes`, `H2CHandler`, and the fluent
`DescribeBuilder`.

Wave 5 (`0d66d03` + `ae8893e`) extracted the SDK and migrated the 3
reference plugins. Wave 6 (`ba02e35`) migrated the remaining 11.
Total: -1257 LOC across 11 plugins, ~39% per-plugin reduction.
Contract changes (auth header, phone-home payload, SRI format) now
touch one place.

The Dockerfile pattern uses Docker's named build contexts:

```bash
docker build \
  --build-context platform=platform \
  --build-context sdk=sdk-go \
  -f plugins/rat-plugin-*/Dockerfile ...
```

## Consequences

**Positive.** A contract change is a one-PR drive-by — the Wave 7
timing fix is exactly the kind of regression the extraction was
designed to make cheap. New plugin authors copy a 50-line `main.go`
instead of a 150-line one.

**Negative — coordination tax on `go.mod` sync.** The local `replace`
directive means every plugin's `go.sum` must be regenerated when
`sdk-go`'s dependencies change. `go mod tidy` inside each plugin
directory handles it, but the fan-out is real (commit `267a4c6` was
exactly that — committing the tidied `go.mod`/`go.sum` for 3 migrated
plugins). External module publication (`github.com/rat-data/rat-sdk-go`
with semver tags) is the obvious next step but explicitly deferred to
Wave 8+; the SDK is too young to commit to a versioned external API.

## Related

- ADR-020 — platform_token (the contract `sdk-go` first consolidated).
- [`sdk-go/README.md`](../../sdk-go/README.md) — usage reference.
- [`docs/PLUGIN_AUTHOR_GUIDE.md`](../PLUGIN_AUTHOR_GUIDE.md) — author-facing
  quickstart.
