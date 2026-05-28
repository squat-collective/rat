# Security — top priority, always

> This rule has **no `paths:`** on purpose. Security is cross-cutting, so it
> loads every session. Treat these as blocking requirements, not aspirations.

## Non-negotiables
- **No secrets in code.** Credentials come from env vars / the secrets plugin. The only credential-looking strings allowed in tracked files are the documented dev defaults (`minioadmin`, `rat:rat`, `test-access-key`) — and those are allowlisted in `.gitleaks.toml`. Never add a real secret to a tracked file.
- **Parameterised SQL only.** sqlc (Go) / parameterised DuckDB (Python). Never string-interpolate into SQL.
- **Validate all input at API boundaries** (ratd handlers). Reject before it reaches a store or the runner.
- **Pipeline trust model:** Python pipelines are second-party trusted; the runner container hardening is the boundary, the `exec()` blocklist is defense-in-depth only. Never expose a "submit arbitrary code" path to end users. (`.claude/rules/python.md`, ADR-017.)
- **Non-root containers, read-only FS** where possible; restrictive CORS in prod.

## The gates (enforcement, not vibes)
- `make security-secrets` — gitleaks secret scan, **blocking**. Runs in `make ci-quick` and the `git push` hook, so a secret can't leave the machine.
- `make security` — adds dependency-vuln scans (govulncheck / npm audit / pip-audit), **report-only for now**. Runs in `make ci` + the CI `security` job. When you add or bump a dependency, run it and triage anything new in an issue; promote a scanner to blocking once its baseline is clean.
- New secret-shaped value that's a *legitimate* public default → add it to `.gitleaks.toml`'s allowlist with a comment, never silence the whole scan.

## When reviewing or shipping
Security is a first-class section in the PR template and the `code-reviewer` agent. A change that touches auth, input handling, SQL, secrets, or the trust boundary gets explicit security scrutiny before it merges — not after.
