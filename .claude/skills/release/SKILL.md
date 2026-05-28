---
name: release
description: Cut a RAT release — sync versions, verify CI is green, create and push the git tag, watch the release pipeline. Explicit-invoke only (it publishes).
arguments:
  - version
disable-model-invocation: true
---

Cut release `$version` (e.g. `0.2.0-beta.1`). This publishes to GHCR/npm/GitHub Release — confirm with the user before the tag push.

1. **Version sync** — set every component to `$version` (PEP 440 form for Python, e.g. `0.2.0b1`):
   `runner/pyproject.toml`, `query/pyproject.toml`, `sdk-typescript/package.json`, `portal/package.json`, and **both package-lock.json `version` fields** (CI `npm ci` fails on mismatch). Do NOT hardcode it in `health.go` — ratd's version is ldflags-injected from the tag.
2. **Gate** — `make ci` must be green. Re-lock uv if a Python version changed (`uv lock`).
3. **Branch** — releases tag `main`. If work is on a feature branch, confirm the merge strategy first (PR → CI green → merge), per `.claude/rules/ci.md`.
4. **Tag** — `git tag -a v$version -m "..."`, then **confirm**, then `git push origin v$version` → triggers `release.yml`.
5. **Watch** — `gh run watch` the release run; report the published image/package URLs.

Stop and ask before step 4's push — it's the irreversible publish.
