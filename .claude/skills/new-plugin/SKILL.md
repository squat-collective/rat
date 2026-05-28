---
name: new-plugin
description: Scaffold a new RAT plugin from the canonical template. Args — the plugin name and kind (go|python).
arguments:
  - name
  - kind
disable-model-invocation: true
---

Scaffold `plugins/rat-plugin-$name` (kind = `$kind`).

Follow `.claude/rules/plugins.md`. For a substantial build, hand off to the **plugin-builder** agent (it works in an isolated worktree). For a quick scaffold, do it inline:

**If `$kind` = go** (platform plugin) — crib from `plugins/rat-plugin-secrets/`:
- `go.mod` (with the `/platform` + `/sdk-go` replaces), `main.go` (sdk-go skeleton), `handler.go`, `api.go`, `bundle.js` (two-arg `__RAT_REGISTER_PLUGIN`), `Dockerfile`, `Makefile`, `README.md`.
- Pick an unused `GRPC_PORT` (check existing plugins' Makefiles).

**If `$kind` = python** (runner extension) — crib from `plugins/rat-plugin-soft-delete/`:
- `pyproject.toml` with the right `[project.entry-points."rat.<group>"]` (strategies | pipeline_types | sources | hooks | jinja_helpers), `src/rat_plugin_$name/`, `tests/`, `README.md`.

After scaffolding: `cd plugins/rat-plugin-$name && make build` (go) or `pip install -e '.[dev]' && pytest` (python). Don't touch the compose overlay unless asked.
