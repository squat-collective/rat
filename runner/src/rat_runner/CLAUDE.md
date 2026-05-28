# rat-runner internals — non-obvious wiring

(Python conventions + the sandbox trust model are in `.claude/rules/python.md`; this is the execution-flow orientation that isn't obvious from one file.)

## Every run is branch-isolated (executor.py, 5 phases)
A pipeline run does NOT write to `main` directly. It:
1. **Branch** — create an ephemeral Nessie branch `run-<uuid>` off `main`.
2. **Write** — execute the SQL/Python against DuckDB, write the Iceberg table *on that branch*.
3. **Test** — run quality tests (`tests/quality/*.sql`); failure aborts before merge.
4. **Merge** — fast-forward/merge the branch into `main` on success.
5. **Resolve** — on merge failure the branch is **retained** for recovery (a failed-merge audit row is POSTed to ratd's internal listener), not discarded.

So a failed run never poisons `main`, and a "stuck" table usually means a retained run-branch.

## The genesis gotcha
The merge in phase 4 needs a real common ancestor. A **fresh Nessie** starts `main` at zero commits, and merging a descendant of an empty genesis 404s with "no common ancestor" → the first pipeline run fails. The `nessie-init` container (compose) seeds a bootstrap commit; that's why ratd/runner depend on it via `service_completed_successfully` (see `.claude/rules/infra.md`).

## Status callbacks
The runner POSTs terminal status to `RATD_CALLBACK_URL` — which must be ratd's **internal** listener (`http://ratd:8090`), not `:8080`. Wrong port → callbacks 404 → runs hang "running".

## Single-shot mode
`RUN_MODE=single` (set by the Pro container-executor) skips the gRPC server: read params from env, execute once, print a JSON result, exit. Same `execute_pipeline()` as server mode.
