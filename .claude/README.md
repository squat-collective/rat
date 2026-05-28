# `.claude/` — Claude Code project setup

How RAT's repo configures Claude Code (and any agent that reads these). Committed to version control so the whole team shares it; only `settings.local.json` and `worktrees/` are gitignored.

## Layout

```
.claude/
├── rules/            # path-scoped conventions (lazy-loaded by file glob)
├── settings.json     # permissions + hooks (shared, committed)
├── hooks/            # hook scripts (called by settings.json)
├── agents/           # specialized subagents (model-tiered)
├── skills/           # invocable procedures (/command form)
└── worktrees/        # git worktrees for parallel work (gitignored)
```

## Rules (`rules/*.md`)

The lean `CLAUDE.md` at repo root holds only cross-cutting conventions. Everything language/area-specific lives here, each file scoped with a `paths:` glob in its frontmatter so it **loads only when the matching files are touched** (real context savings, unlike `@imports`). Files: `go`, `python`, `typescript`, `proto`, `plugins`, `infra`, `ci`.

Rules are *guidance* (the agent reads them). *Enforcement* is hooks + permissions.

## Settings + hooks (`settings.json`, `hooks/`)

- **Permissions** auto-allow safe, repetitive commands (`make:*`, `gh run:*`, `docker compose:*`) and deny destructive ones (`.env` edits, force-push, `rm -rf`).
- **Hooks** (all call `make` targets — single entry point):
  - `PostToolUse` on edits → format the touched file (`gofmt` / `ruff` / `eslint --fix`).
  - `PreToolUse` on `git push` → `make ci-quick`; **blocks the push if it fails.** Escape hatch: `RAT_SKIP_CI=1 git push` for genuine emergencies.
  - `SessionStart` → print branch + dev-stack status.

`settings.local.json` (gitignored) is for personal overrides.

## Agents (`agents/*.md`)

Model-tiered specialists: `ci-doctor` (diagnose failed CI runs), `code-reviewer` (deep review, Opus), `plugin-builder` (scaffold a plugin in an isolated worktree).

## Skills (`skills/*/SKILL.md`)

Invocable procedures: `/ci-local`, `/new-plugin`, `/ship-wave`, `/release`, plus a `paths`-scoped `plugin-authoring` helper that auto-surfaces inside `plugins/`.
