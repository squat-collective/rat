---
paths:
  - "plugins/**"
---

# Plugin rules

Two distinct plugin layers — don't conflate them:

| Layer | Shape | Loaded by |
|---|---|---|
| **Runner plugin** | Python pkg (`pyproject.toml`, no Dockerfile) | pip + Python `entry_points` (`rat.strategies`, `rat.pipeline_types`, `rat.sources`, `rat.hooks`, `rat.jinja_helpers`) |
| **Platform plugin** | Go container (has `Dockerfile`) | phones home to ratd's internal listener; ratd reverse-proxies its HTTP at `/api/v1/x/<name>/*` |

A "plugin" usually means the **platform** (Go-container) kind.

## Platform-plugin skeleton (canonical: `rat-plugin-secrets/main.go`)
1. `sdk.LoadPluginEnv(name, port, addr)` — reads `PLUGIN_NAME/ADDR`, `GRPC_PORT`, `RATD_URL`, `RATD_INTERNAL_URL`.
2. `sdk.PhoneHomeLoop(env.RatdInternalURL, name, addr)` — registers with backoff.
3. `sdk.MountStandardPluginRoutes(mux, handler, bundleJS, platformToken, apiMux)`.
4. `sdk.NewDescribe(name, version, summary).WithRoute(...).WithUI(...).WithPlatformToken(sdk.RandomToken()).Build()`.
5. Per-startup random `platform_token` advertised via Describe; ratd injects it as `X-RAT-Plugin-Token`. Validate with `sdk.TokenAuth` (constant-time).

`go.mod` uses `replace … => /platform` and `=> /sdk-go`; the Dockerfile supplies those via `--build-context platform=… sdk=…`.

## UI bundle gotcha
`bundle.js` is build-free (hand-written `React.createElement`, no JSX). Register with the **two-arg** form:
`window.__RAT_REGISTER_PLUGIN("name", { navItems: [...], routes: [{ path, component }] })`.
The one-object form crashes the loader with `Cannot read properties of undefined (reading 'slots')`.

## Distribution
Source lives here; CI (`publish-plugins.yml`) publishes `ghcr.io/squat-collective/rat-plugin-<name>` on push-to-main + `plugin-<name>-v*` tags. Install via the image (or `infra/docker-compose.plugins.yml` overlay). Local dev: `cd plugins/rat-plugin-<name> && make build && make run`.

## When to add a plugin vs core
Plugin: optional feature, own UI surface (`/x/<name>`), external-system integration, or experimental. Core: load-bearing for the standard workflow, needs deep ratd state, or decoupling cost > optionality benefit.

## Iceberg from a plugin (compaction precedent)
PyIceberg 0.11.x has **no `rewrite_data_files`** — use `tbl.overwrite(tbl.scan().to_arrow())`. Detect file counts from the **current snapshot's `metadata.json` summary** (`total-data-files`), NOT a raw S3 listing (raw includes orphaned files from old snapshots → infinite re-compaction loop). MinIO needs AWS-V4 signing — use the MinIO Go SDK, not basic auth.
