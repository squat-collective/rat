---
name: ship-wave
description: Run the wave-shipping workflow — verify make ci is green, commit the work in the house format, and update project memory. Explicit-invoke only.
disable-model-invocation: true
---

Ship the current wave of work cleanly:

1. **Gate:** run `make ci`. If red, stop and report — do not commit.
2. **Commit:** stage the relevant files (by name, not `git add -A`) and write a commit in the house format: `<type>(<scope>): <desc>`, body explaining the *why*, ending with `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`. One logical commit per concern.
3. **Memory:** if the wave produced a durable, non-obvious learning (a new pattern, a gotcha, an architecture decision), record it via the auto-memory system — not routine code changes.
4. Report: commits created, ci status, what's next.

Do NOT push or merge — that's the user's call (and the push hook runs ci-quick anyway).
