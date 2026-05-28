---
name: ci-local
description: Run the full local CI mirror (`make ci`) — lint + golangci + all Go/Python/TS unit tests — and report results. Use before opening or merging a PR, or when the user asks to "check CI locally".
allowed-tools: Bash(make:*), Read
---

Run `make ci` (the pinned-tool, CI-identical gate). Report pass/fail **per gate** (lint, golangci, go tests, python tests, ts tests).

On failure: show the failing gate's error, name the likely cause, and point at the `.claude/rules/` file that owns it (e.g. ruff → `python.md`, golangci → `go.md`). Do not auto-fix unless asked — offer to hand off to the `ci-doctor` agent for diagnosis.

If the user only touched one area, note that `make ci-quick` (lint + Go tests) is the faster gate.
