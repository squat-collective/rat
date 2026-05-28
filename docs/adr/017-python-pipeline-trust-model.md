# ADR-017: Python pipeline trust model

## Status: Accepted (2026-05-27)

## Context

RAT supports two pipeline source languages: SQL (Jinja-templated, run by DuckDB) and
Python (run via `exec()` in `runner/src/rat_runner/python_exec.py`). The Python path
exists because some transforms — irregular CSV/JSON normalisation, calls into PyArrow
compute kernels, ad-hoc reshaping — are genuinely awkward to express in SQL. Operators
write a `pipeline.py` that assigns a PyArrow `Table` to a `result` variable; the runner
loads it from S3 and executes it.

The Python source is **operator-authored code stored in the platform's S3 bucket**. It
arrives in the runner via `read_s3_text(s3_config, ".../pipeline.py")` on the normal
run path, or via the preview API's `code` field when the operator hits "Preview" in the
portal IDE. There is no public "submit arbitrary Python" endpoint — the IDE itself is
gated by the platform's auth middleware.

The current `python_exec.py` sandbox uses four blocklist mechanisms:

1. **`_BLOCKED_BUILTINS`** — strips `eval`, `exec`, `open`, `getattr`, `globals`, etc.
   out of the exec'd globals dict
2. **`_BLOCKED_MODULES`** — `_restricted_import` rejects `os`, `sys`, `subprocess`, …
3. **`_BLOCKED_DUNDERS`** — `_DunderAccessChecker` walks the AST and rejects
   `__class__`, `__subclasses__`, `__globals__`, … (string-literal variants too)
4. **`_DANGEROUS_SQL_PATTERN`** — a `_SafeDuckDBConnection` wrapper matches `COPY`,
   `ATTACH`, `INSTALL`, `LOAD`, … against every SQL string before forwarding to DuckDB

This is a textbook blocklist sandbox. Blocklist sandboxes for CPython are historically
a losing race against attacker patience — `getattr` is closed, `__subclasses__` is
closed, then someone finds the next gadget. We should be honest about what these
layers are actually for.

## Decision

**Python pipelines are second-party trusted code (operator-written or
operator-reviewed). The runner container — not the `exec()` blocklist — is the real
security boundary.** The blocklist is defense-in-depth, not the security boundary.

Two direct consequences:

1. **Never expose a "submit arbitrary Python" API to end users.** The preview `code`
   field on `POST /pipelines/{ns}/{layer}/{name}/preview` is intended for the
   operator's IDE only; the auth middleware is the gate. Any plugin or extension that
   reuses `executor.execute()` / `execute_python_pipeline()` to run user-supplied code
   introduces an RCE vector.
2. **Tightening the blocklist is welcome but not load-bearing.** Closing new gadgets
   is fine. New threat models (multi-tenant code submission, third-party plugin
   pipelines, marketplace-style sharing of `pipeline.py`) MUST NOT be served by the
   current blocklist; they need a real isolation layer (see Alternatives).

## Consequences

### Positive

- Simpler reasoning. The container hardening in `infra/docker-compose.yml`
  (`read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`,
  memory + pid limits) does the heavy lifting. A successful `exec()` escape lands in
  a read-only, capability-dropped, no-new-privs container with bounded RAM and PIDs.
- We stop chasing CPython gadgets as a security activity. Bug reports against the
  blocklist are triaged as hardening, not as "RCE-class".

### Negative

- An operator who accidentally exposes a "let users submit Python" endpoint via a
  plugin or API extension introduces an RCE vector. **Mitigation:** any code review
  touching `runner/src/rat_runner/python_exec.py`, or any new caller of
  `execute_python_pipeline()` / `executor.execute()` for Python pipelines, MUST flag
  the trust-boundary assumption in the PR description.
- The four blocklist mechanisms are now explicitly second-class. Future contributors
  may be tempted to delete them. They stay — they catch honest mistakes, slow down
  curious operators, and surface obviously-wrong code at validation time.

## Alternatives considered

- **RestrictPython / PyPy-based sandbox.** Heavier dependency, brittle (RestrictPython
  has its own escape history), and ultimately the same fundamental category of
  defence as our blocklist. Not a meaningful upgrade.
- **gVisor / Firejail / seccomp at the OS layer.** This is the right answer if we
  ever DO need to accept untrusted code — runsc-style user-space kernel or a
  seccomp-bpf profile around the runner. Documented here as the escape hatch; not
  built today because the trust model doesn't require it.
- **DSL replacement (DuckDB SQL only, no Python).** Viable for a Pro Edition tier
  that wants the strongest claim ("no arbitrary code execution, ever"). Drops a real
  feature operators use. Out of scope for this ADR.

## Related

- `runner/src/rat_runner/python_exec.py` — the implementation, with a docstring
  pointing back to this ADR.
- `infra/docker-compose.yml` — the actual trust boundary (`runner` service block).
- ADR-009 (Container Executor) — Pro-only per-run container isolation; complementary,
  not a replacement.
