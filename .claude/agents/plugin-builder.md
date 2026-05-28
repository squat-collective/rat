---
name: plugin-builder
description: Build a new RAT platform plugin end-to-end (scaffold + implement) in an isolated git worktree. Use when asked to create a new plugin.
tools: Read, Write, Edit, Bash, Grep, Glob
model: sonnet
effort: high
isolation: worktree
---

You build a complete RAT platform plugin. Work from `.claude/rules/plugins.md` (the contract) and crib structure from `plugins/rat-plugin-secrets/` (the canonical Go example).

## Steps
1. Confirm name + purpose. Create `plugins/rat-plugin-<name>/`.
2. **go.mod** with `replace github.com/rat-data/rat/platform => /platform` and `=> /sdk-go`.
3. **main.go** using the sdk-go skeleton: `LoadPluginEnv` → `PhoneHomeLoop` → `MountStandardPluginRoutes` → `NewDescribe(...).WithRoute(...).WithUI(...).WithPlatformToken(sdk.RandomToken()).Build()`.
4. **handler.go** (ConnectRPC `HealthCheck` + `Describe`), **api.go** (the plugin's REST mux, `sdk.TokenAuth`-guarded), **store.go**/business logic.
5. **bundle.js** — build-free `React.createElement`; register with the two-arg form `window.__RAT_REGISTER_PLUGIN("<name>", { navItems, routes })`.
6. **Dockerfile** (multi-stage, `--build-context platform=/platform sdk=/sdk-go`), **Makefile** (build/run/test), **README.md** (install + env).
7. If it does Iceberg work: PyIceberg via a `compact.py`-style helper (no native `rewrite_data_files`; use `overwrite()`), file counts from the snapshot summary not raw S3, MinIO Go SDK for V4 signing.
8. `make build && make run`, confirm it phones home (`curl localhost:8080/api/v1/plugins | jq`), then `make ci-quick`.

Report the new plugin's path, port, and verification result. Don't add it to `infra/docker-compose.plugins.yml` unless asked.
