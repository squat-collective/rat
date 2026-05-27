// Package leader provides Postgres advisory lock-based leader election.
// When multiple ratd replicas are running, only the leader should start
// background workers (scheduler, trigger evaluator, reaper) to avoid
// duplicate pipeline runs.
//
// The leader acquires a Postgres advisory lock (pg_try_advisory_lock) and
// periodically retries if the lock is not acquired. Postgres releases an
// advisory lock only when the holding session ends; a network-partitioned
// leader could keep the lock indefinitely while no background workers run
// on any replica. To bound that worst case, the leader runs a heartbeat
// goroutine that pings Postgres every heartbeatInterval. Two consecutive
// failures trigger a voluntary release of the advisory lock so another
// replica can take over on its next poll cycle. Graceful shutdown also
// explicit-unlocks the advisory lock instead of relying on session death.
package leader

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// AdvisoryLockID is a fixed int64 used as the Postgres advisory lock key.
// Chosen to avoid collisions with the migration lock (779415198).
const AdvisoryLockID int64 = 7526700533049

// RetryInterval is the default interval between leader election retry attempts.
const RetryInterval = 30 * time.Second

// DefaultHeartbeatInterval is how often the leader pings Postgres to prove
// it can still reach the database. Two consecutive ping failures trigger a
// voluntary release of the advisory lock.
const DefaultHeartbeatInterval = 5 * time.Second

// heartbeatFailureThreshold is the number of consecutive ping failures that
// causes the leader to voluntarily step down.
const heartbeatFailureThreshold = 2

// TryLockFunc attempts to acquire the advisory lock.
// Returns true if the lock was acquired, false if another session holds it.
// In production, the caller provides this using pgxpool.Pool.QueryRow:
//
//	leader.New(func(ctx context.Context) (bool, error) {
//	    var acquired bool
//	    err := pool.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", leader.AdvisoryLockID).Scan(&acquired)
//	    return acquired, err
//	}, ...)
type TryLockFunc func(ctx context.Context) (acquired bool, err error)

// UnlockFunc releases the advisory lock held by the current session.
// In production, the caller provides this using pgxpool.Pool.Exec:
//
//	func(ctx context.Context) error {
//	    _, err := pool.Exec(ctx, "SELECT pg_advisory_unlock($1)", leader.AdvisoryLockID)
//	    return err
//	}
//
// If nil, the leader cannot voluntarily release the lock and falls back to
// session-death behaviour (Postgres releases on connection close). This is
// supported for backward compatibility but disables the heartbeat safety
// net — production callers should always provide an UnlockFunc.
type UnlockFunc func(ctx context.Context) error

// PingFunc probes the database to prove the leader can still reach it.
// Implementations typically run `SELECT 1`. If nil, the heartbeat goroutine
// does not run (legacy behaviour).
type PingFunc func(ctx context.Context) error

// OnElected is called when this replica becomes the leader.
// It should start background workers. The returned stop function is called
// when leadership is lost (context cancelled, explicit stop, or heartbeat
// failure-triggered voluntary release).
type OnElected func(ctx context.Context) (stop func())

// Option configures an Elector at construction time.
type Option func(*Elector)

// WithUnlock supplies the function used to voluntarily release the advisory
// lock on heartbeat failure or graceful shutdown.
func WithUnlock(fn UnlockFunc) Option {
	return func(e *Elector) { e.unlock = fn }
}

// WithPing supplies the function used by the heartbeat goroutine to verify
// the leader can still reach Postgres. Required for heartbeat-based liveness.
func WithPing(fn PingFunc) Option {
	return func(e *Elector) { e.ping = fn }
}

// WithHeartbeatInterval overrides the heartbeat tick interval. Used by tests
// to compress timing; production should use the default.
func WithHeartbeatInterval(d time.Duration) Option {
	return func(e *Elector) { e.heartbeatInterval = d }
}

// Elector manages leader election using Postgres advisory locks.
// It periodically tries to acquire the lock and calls OnElected when
// leadership is gained. While elected, a heartbeat goroutine probes the
// database and forces a voluntary step-down if liveness can no longer be
// proven.
type Elector struct {
	tryLock           TryLockFunc
	unlock            UnlockFunc
	ping              PingFunc
	retryInterval     time.Duration
	heartbeatInterval time.Duration
	onElected         OnElected

	mu              sync.Mutex
	isLeader        bool
	stopFn          func() // stop function returned by OnElected
	heartbeatCancel context.CancelFunc
	heartbeatDone   chan struct{}
	cancel          context.CancelFunc
	done            chan struct{}
}

// New creates an Elector that will try to acquire leadership using the given
// lock function. When elected, onElected is called with a context that remains
// valid for the duration of leadership. retryInterval controls how often a
// non-leader replica retries acquiring the lock.
//
// Without options the Elector preserves legacy behaviour: no heartbeat, no
// explicit unlock. Production callers should pass WithPing and WithUnlock
// so a network-partitioned leader can voluntarily step down.
func New(tryLock TryLockFunc, retryInterval time.Duration, onElected OnElected, opts ...Option) *Elector {
	e := &Elector{
		tryLock:           tryLock,
		retryInterval:     retryInterval,
		heartbeatInterval: DefaultHeartbeatInterval,
		onElected:         onElected,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Start begins the leader election loop in a background goroutine.
// It immediately tries to acquire the lock, then retries at the configured
// interval if not acquired.
func (e *Elector) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	e.done = make(chan struct{})

	go func() {
		defer close(e.done)

		// Try immediately on startup.
		e.tryAcquire(ctx)

		ticker := time.NewTicker(e.retryInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Use Background for the unlock attempt; the parent ctx
				// is already cancelled and would refuse the query.
				e.relinquish(context.Background(), false)
				return
			case <-ticker.C:
				e.tryAcquire(ctx)
			}
		}
	}()
}

// Stop cancels the election loop and waits for it to finish.
// If this replica is the leader, it calls the stop function from OnElected
// and explicitly releases the advisory lock (if UnlockFunc was provided).
func (e *Elector) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	if e.done != nil {
		<-e.done
	}
}

// IsLeader returns whether this replica currently holds the leader lock.
func (e *Elector) IsLeader() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.isLeader
}

// tryAcquire attempts to acquire the advisory lock if not already the leader.
func (e *Elector) tryAcquire(ctx context.Context) {
	e.mu.Lock()
	if e.isLeader {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	acquired, err := e.tryLock(ctx)
	if err != nil {
		slog.Error("leader: failed to try advisory lock", "error", err)
		return
	}

	if !acquired {
		slog.Debug("leader: lock not acquired, another replica is leader")
		return
	}

	slog.Info("leader: advisory lock acquired, starting background workers")

	e.mu.Lock()
	e.isLeader = true
	e.mu.Unlock()

	stopFn := e.onElected(ctx)

	e.mu.Lock()
	e.stopFn = stopFn
	e.mu.Unlock()

	// Start heartbeat goroutine if a PingFunc was provided. The heartbeat
	// runs for the lifetime of leadership; it is cancelled by relinquish().
	if e.ping != nil {
		e.startHeartbeat(ctx)
	}
}

// startHeartbeat launches the heartbeat goroutine that pings Postgres on a
// fixed interval. Two consecutive failures cause a voluntary step-down.
func (e *Elector) startHeartbeat(parent context.Context) {
	hbCtx, hbCancel := context.WithCancel(parent)
	done := make(chan struct{})

	e.mu.Lock()
	e.heartbeatCancel = hbCancel
	e.heartbeatDone = done
	e.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(e.heartbeatInterval)
		defer ticker.Stop()

		var consecutiveFailures int
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				err := e.ping(hbCtx)
				if err != nil {
					consecutiveFailures++
					slog.Warn("leader: heartbeat ping failed",
						"error", err,
						"consecutive_failures", consecutiveFailures,
						"threshold", heartbeatFailureThreshold)
					if consecutiveFailures >= heartbeatFailureThreshold {
						slog.Error("leader: heartbeat threshold exceeded, voluntarily releasing advisory lock",
							"consecutive_failures", consecutiveFailures)
						// Relinquish on a fresh context — the parent may be
						// the same context Postgres is unreachable on, but
						// the unlock attempt must still be allowed to run
						// (and fail) so consumers learn we stepped down.
						//
						// fromHeartbeat=true so relinquish doesn't wait on
						// our own goroutine to close (we ARE that goroutine).
						e.relinquish(context.Background(), true)
						return
					}
				} else {
					if consecutiveFailures > 0 {
						slog.Info("leader: heartbeat recovered",
							"prior_consecutive_failures", consecutiveFailures)
					}
					consecutiveFailures = 0
					slog.Debug("leader: heartbeat ok")
				}
			}
		}
	}()
}

// relinquish stops background workers if this replica is the leader and
// explicitly releases the advisory lock (when UnlockFunc was provided).
// Safe to call multiple times.
//
// fromHeartbeat must be true when relinquish is invoked from inside the
// heartbeat goroutine itself; in that case we must not wait on heartbeatDone
// (the goroutine is the caller — waiting would deadlock). Otherwise we wait
// so any in-flight ping completes before workers are stopped.
func (e *Elector) relinquish(ctx context.Context, fromHeartbeat bool) {
	e.mu.Lock()

	if !e.isLeader {
		e.mu.Unlock()
		return
	}

	slog.Info("leader: relinquishing leadership, stopping background workers")

	// Cancel the heartbeat first so it doesn't trigger a recursive
	// relinquish if it happens to tick during shutdown.
	heartbeatCancel := e.heartbeatCancel
	heartbeatDone := e.heartbeatDone
	stopFn := e.stopFn
	unlock := e.unlock

	e.heartbeatCancel = nil
	e.heartbeatDone = nil
	e.stopFn = nil
	e.isLeader = false
	e.mu.Unlock()

	if heartbeatCancel != nil {
		heartbeatCancel()
	}
	if heartbeatDone != nil && !fromHeartbeat {
		// Wait for the heartbeat goroutine to exit before stopping workers
		// so any in-flight ping completes and we can't race with another
		// failure threshold trip.
		<-heartbeatDone
	}

	if stopFn != nil {
		stopFn()
	}

	if unlock != nil {
		if err := unlock(ctx); err != nil {
			// Log but don't surface — even on failure, the session-death
			// fallback eventually releases the lock.
			slog.Warn("leader: failed to release advisory lock; relying on session-death fallback",
				"error", err)
		} else {
			slog.Info("leader: advisory lock released")
		}
	}
}
