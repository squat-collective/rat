# ADR-023: Leader heartbeat with dedicated pool

## Status: Accepted (2026-05-27)

## Context

ratd uses Postgres `pg_try_advisory_lock` to elect a single leader for
background work (scheduler, trigger evaluator, reaper). The lock is
held for the session's lifetime — Postgres releases it only when the
session dies. That leaves an availability hole: a partitioned leader
(alive enough to hold TCP, dead enough to do nothing) keeps the lock
indefinitely while no replica picks up the background work.

Wave 5 (`7980a21`) added a 5-second heartbeat `SELECT 1`; two
consecutive failures trigger a voluntary `pg_advisory_unlock` so a
replica can take over on the next 10-second poll.

Wave 6 (`b333fa4`) found the next layer: the heartbeat was pulling from
the shared pool. Under load — slow analytical queries, handler bursts —
the pool saturated, the heartbeat itself timed out, the voluntary
release fired. Replicas picked up the lock and within seconds *their*
pools saturated too. Leadership ping-ponged every ~10 seconds and the
schedule fired duplicates.

## Decision

Spin a dedicated 1-connection `pgxpool.Pool` solely for the heartbeat —
handler load can't saturate it. `RAT_HEARTBEAT_POOL_ENABLED=false` opts
out for tiny deployments (falls back to the shared pool).

Wave 6 also introduced a `Clock` interface so tests use
`fakeClock.Advance()` instead of `time.Sleep`, eliminating heartbeat
test flakes.

Wave 7 (`e003f7e`) handled the boot ordering: `NewHeartbeatPool` can
fail on first attempt when ratd and Postgres start in parallel (k8s,
slow hosts). The original `os.Exit(1)` crashed boot. Now: 5 attempts
with exponential backoff (1s, 2s, 4s, 8s); on terminal failure, log a
WARN and fall back to the shared pool. The pool-starvation guard is
lost but the platform stays up.

## Consequences

**Positive.** Leadership is stable under handler load. One extra
Postgres connection per replica — cheap. Tests are deterministic.

**Negative — accepted.** When the dedicated pool fails to initialise we
log a WARN and *silently* lose the saturation guard. Operators who care
must alert on the WARN. The alternative (refuse to start) is worse
because most deployments don't need the guard and ratd would be brittle
to boot-time races. The trade is "platform stays up, monitoring catches
the degradation."

## Related

- ADR-022 — InTx helper (the *other* "be careful with the shared pool"
  story, from the same cluster of pool-saturation issues).
- [`platform/internal/leader/leader.go`](../../platform/internal/leader/leader.go)
  — the heartbeat implementation.
