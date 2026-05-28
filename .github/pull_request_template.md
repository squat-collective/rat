## Summary

<!-- What changed and WHY (not just what). 1–3 bullets. -->

## Test plan

- [ ] `make ci` green locally (lint + golangci + security + docs-check + Go/Python/TS tests)
- [ ] <!-- anything reviewers should manually verify -->

## Docs — required

Pick the boxes that apply (or check N/A and say why):

- [ ] Inline godoc/docstring updated for new/changed public APIs
- [ ] New env vars added to `docs/config.md`; new endpoints noted in `docs/api-spec.md`
- [ ] Proto field/message changes commented
- [ ] ADR added in `docs/adr/` for a significant design decision
- [ ] Migration note in `docs/migrations/` for a breaking change
- [ ] N/A — no doc-affecting changes (explain): <!-- … -->

## Security — required

- [ ] `make security` run; gitleaks clean (no new secrets)
- [ ] Input validated at API boundaries; SQL parameterised (sqlc / parameterised DuckDB)
- [ ] New/bumped dependencies triaged against govulncheck / npm audit / pip-audit
- [ ] Touches auth / trust boundary / secrets? Called out for review: <!-- … -->
- [ ] N/A — no security-relevant changes (explain): <!-- … -->

🤖 Generated with [Claude Code](https://claude.com/claude-code)
