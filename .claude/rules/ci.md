---
paths:
  - ".github/workflows/**"
  - "Makefile"
  - "**/pyproject.toml"
  - "**/go.mod"
  - "**/package.json"
---

# CI & tooling rules

## Make is the single entry point
Every recurrent command goes through `make` — no raw `docker run`/`go test`/`pytest`/`npm`. Hooks and skills call `make` targets, never re-implement logic.

## Run `make ci` before pushing — non-negotiable
- `make ci-quick` — all linters + `go test -race`. The `git push` hook runs this and **blocks the push on failure**.
- `make ci` — full mirror: lint + golangci + Go/Python/TS unit suites. Run before opening/merging a PR.
- These exist because the recurring failure mode was "local ≠ CI." Keep them in lockstep with `.github/workflows/ci.yml`.

## Pinned tooling (the whole point — never use `latest`)
| Tool | Pin | Where |
|---|---|---|
| golangci-lint | `v2.12.2` | `GO_LINT_IMAGE` + `ci.yml` golangci action (config: `platform/.golangci.yml`, schema v2) |
| ruff | `0.15.15` | Makefile `RUFF_VERSION` + both `pyproject` dev + `ci.yml` pipx |
| buf python codegen | `v33.0` | `proto/buf.gen.yaml` |
| Go | `1.26` | go.mod + images |
| Node | `20` | images |
| protobuf (py runtime) | `>=6.33,<7` (via lock) | runner/query |

An unpinned linter is a future "green build silently breaks." If you bump a pin, re-run `make ci` and update every location in this table together.

## CI structure
Jobs: Go Tests · Python (runner/query) · TypeScript (SDK+portal) · Lint (go vet + golangci + ruff + buf) · **Security** (`make security-secrets` — gitleaks, blocking) · **Docs** (`make docs-check` — lychee links + env-var coverage, blocking) · Docker Build · Integration. `npm ci` is used (lock must match package.json). Plugin-test matrix lives under `plugins/` (not `examples/`). Dependency vuln scans (govulncheck / npm audit / pip-audit) are report-only in `make security` (run locally) + Dependabot — deliberately not a per-push CI job, to save minutes.

## Version comes from the git tag
`release.yml` injects `api.Version` via ldflags (`--build-arg VERSION=`); pyproject/package.json versions are synced at publish. Never hardcode a release version in source — the tag is the single source of truth. Series is `0.x` (SemVer 0.y while the plugin contract isn't frozen); "v2" is a codename, not the version.

## GH Actions cost
Docker Build is ~50% of each run's minutes (it builds 4 images). If trimming cost: make it PR-only + path-filtered, and limit `on: push` to `[main]`.
