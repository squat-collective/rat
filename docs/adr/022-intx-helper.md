# ADR-022: InTx helper for cross-statement atomicity

## Status: Accepted (2026-05-27)

## Context

Two handlers in `platform/internal/api/` performed multi-step DB writes
the third deep review flagged as non-atomic:

1. `HandleWebhookTrigger` — `CreateRun` followed by `UpdateTriggerFired`.
   A crash between them leaves a pending run whose trigger looks
   unfired; the next evaluator pass fires a duplicate.
2. `fireTriggerIfReady` (the cron evaluator) — same shape, same race.

Both paths predate the transaction helper. `sqlc` generates
per-statement `Queries`; there was no convenient way to bind them to a
`pgx.Tx`.

## Decision

Add `postgres.InTx(ctx, pool, fn func(pgx.Tx) error) error` as the
low-level helper, and `postgres.TxRunner.InTx(ctx, fn func(api.TxStores)
error) error` as the high-level API. The runner exposes tx-bound `Runs`
/ `Triggers` / `Schedules` stores so handlers don't import `pgx`
directly. Rollback on error or panic; commit on clean return.

The contract is **"fn must only do DB work"** — and the godoc is
explicit. HTTP (or any IO that yields) inside the tx holds a pooled
connection across the network and can starve the pool. The webhook
handler obeys: `CreateRun` and `UpdateTriggerFired` happen inside the
tx; `Executor.Submit` runs **after** commit.

Implemented in `9e0ac97`. The remaining 70+ handlers stay
single-statement — that's already atomic at the SQL level.

## Consequences

**Positive.** The two duplicate-run races are closed by construction.
The `TxStores` interface keeps handler code unchanged in shape —
`stores.Runs.CreateRun(...)` reads identically inside or outside the
tx.

**Negative — the IO-inside-tx footgun is operator-trust enforced, not
type-enforced.** Nothing in Go's type system stops someone from calling
`http.Post` inside the callback. We deliberately did NOT block the
shared pool by passing a different `*pgxpool.Pool` to the executor
because that would surface as a 502 instead of a code-review catch.
Mitigation: the contract is loud in the godoc and the two existing
call-sites are explicit about doing HTTP after `InTx` returns.

## Related

- ADR-023 — leader heartbeat with dedicated pool (the *other* place a
  pool-starvation pattern bit us, motivating "be careful with the shared
  pool" as a recurring theme).
- [`platform/internal/postgres/tx.go`](../../platform/internal/postgres/tx.go)
  — the helper.
