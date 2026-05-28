# 🐀 RAT — Development Guidelines

> *"In the sewer, we transform data faster than anyone above ground."*

RAT is a self-hostable data platform. Community Edition is free, open-source, single-user. Pro Edition adds multi-user, sharing, and cloud via closed-source container plugins. **"v2"** is the codename for the full rewrite, *not* the SemVer version — releases are `0.x` (the plugin contract isn't frozen yet). Stack: Go platform (`ratd`) + Python execution (runner, query) + Next.js portal.

## Where the rules live

This file holds only **cross-cutting** conventions that apply to every change. Language- and area-specific rules are **path-scoped** in `.claude/rules/` and load automatically when you touch matching files:

| Rule file | Applies when editing |
|---|---|
| `.claude/rules/go.md` | `platform/**/*.go` |
| `.claude/rules/python.md` | `runner/`, `query/` Python |
| `.claude/rules/typescript.md` | `portal/`, `sdk-typescript/` |
| `.claude/rules/proto.md` | `proto/**` |
| `.claude/rules/plugins.md` | `plugins/**` |
| `.claude/rules/infra.md` | `infra/`, Dockerfiles, compose |
| `.claude/rules/ci.md` | workflows, Makefile, manifests |

Claude-specific automation (hooks, agents, skills) lives under `.claude/` — see `.claude/README.md`.

---

## Architecture

```
ratd (Go)       — API, auth, scheduling, plugin host, catalog, storage ops
ratq (Python)   — interactive DuckDB queries (read-only), schema introspection
runner (Python) — pipeline execution, DuckDB writes, PyArrow, Iceberg, quality tests
portal (Next.js)— Web IDE (the ONLY user interface)
postgres        — platform state (runs, schedules, ownership)
minio           — S3-compatible object storage
nessie          — Iceberg REST catalog
```

**Communication:** Portal→ratd REST · ratd→ratq/runner/plugins gRPC (ConnectRPC) · ratd→postgres SQL · ratd→minio S3 · ratd→nessie Iceberg REST.

## Repository structure

```
ratatouille/                 # PUBLIC community monorepo
├── platform/                # Go — ratd
├── runner/  query/          # Python — pipeline execution / query sidecar
├── portal/                  # Next.js — web IDE
├── sdk-typescript/  sdk-go/ # SDKs (portal / plugins)
├── proto/                   # shared gRPC protobuf
├── plugins/                 # community plugins (Go containers + Python pkgs)
├── infra/                   # docker compose, configs, scripts
├── docs/                    # architecture docs, ADRs, migrations
└── Makefile                 # root orchestrator
```
`ratatouille-pro/` is a separate private repo (Pro plugins, Keycloak realm).

---

## Git workflow (GitHub Flow)

- **`main` is always green** — never push broken code. The `git push` hook runs `make ci-quick` and blocks red pushes.
- **Short-lived branches, small PRs.** Prefer one worktree + PR per feature/wave (`claude --worktree <name>`) over a long-running mega-branch.
- **Squash merge** to main; delete branches after; **no force-push to main**.
- **Tag releases** with semver (e.g. `v0.2.0-beta.1`). The git tag is the single source of truth for version — never hardcode it in source (see `.claude/rules/ci.md`).
- **Commit format:** `<type>(<scope>): <desc>` — types `feat|fix|test|refactor|docs|chore|ci`, scopes `ratd|runner|ratq|portal|proto|sdk|infra`. End messages with `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`.

## TDD

Write the test first; every bug fix gets a regression test. Prefer real dependencies (test containers, in-memory DBs) over mocks — mock only external services. Target 80%+ on core logic (engine, executor, auth, handlers); don't chase 100%. Naming: Go `TestFunc_Scenario_Expected`, Python `test_reads_like_a_sentence`, TS `describe/it`.

## Definition of done

A change is **done** only when `make ci` is green (lint + golangci + Go/Python/TS unit tests). Don't report work complete on red local checks — that's how CI debt accumulates (and it cost us a full cleanup once).

## Make is the single entry point

Every recurrent command goes through `make` (`make help` lists them). No raw `docker run`/`go test`/`pytest`/`npm`. Hooks and skills call `make` targets, never re-implement logic. The pinned-tooling table + the `make ci`/`ci-quick` gates are in `.claude/rules/ci.md`.

## Documentation

Code without docs is unfinished. Update at the right level in the same PR: inline godoc/docstring (the *why*), proto field comments, `docs/config.md` (env vars), `docs/v2-strategy.md` (architecture — the source of truth), an ADR in `docs/adr/` (significant decisions, immutable once accepted), `docs/migrations/` (breaking changes).

## Security

No secrets in code (env vars only). Validate all input at API boundaries. **Parameterised SQL only** — sqlc (Go) / parameterised DuckDB (Python), never string interpolation. Non-root containers, read-only filesystems where possible. CORS restrictive in prod. The Python pipeline trust model is in `.claude/rules/python.md` (+ ADR-017).

## PR checklist

- [ ] `make ci` green (tests + lint)
- [ ] Test written first (TDD); regression test for any bug fix
- [ ] No `any` (TS) / no `# type: ignore` (Python)
- [ ] Proto changes backward-compatible (`buf breaking`)
- [ ] Docs updated; ADR written if it's a significant design decision
- [ ] PR description explains **why**, not just **what**
- [ ] No secrets / `.env` committed

## Reference

`docs/v2-strategy.md` (architecture source of truth) · `docs/config.md` · `docs/api-spec.md` · `docs/adr/` · `docs/migrations/` · v1 codebase: `~/sandbox/ratatouille/` · Pro plugins: `~/sandbox/ratatouille-v2/ratatouille-pro/`.
