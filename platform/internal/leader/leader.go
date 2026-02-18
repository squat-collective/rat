// Package leader provides Postgres advisory lock-based leader election.
// When multiple ratd replicas are running, only the leader should start
// background workers (scheduler, trigger evaluator, reaper) to avoid
// duplicate pipeline runs.
//
// The leader acquires a Postgres advisory lock (pg_try_advisory_lock) and
// periodically retries if the lock is not acquired. When the leader dies,
// Postgres automatically releases the lock, allowing another replica to
// take over.
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

// OnElected is called when this replica becomes the leader.
// It should start background workers. The returned stop function is called
// when leadership is lost (context cancelled or explicit stop).
type OnElected func(ctx context.Context) (stop func())

// Elector manages leader election using Postgres advisory locks.
// It periodically tries to acquire the lock and calls OnElected when
// leadership is gained.
type Elector struct {
	tryLock       TryLockFunc
	retryInterval time.Duration
	onElected     OnElected

	mu       sync.Mutex
	isLeader bool
	stopFn   func() // stop function returned by OnElected
	cancel   context.CancelFunc
	done     chan struct{}
}

// New creates an Elector that will try to acquire leadership using the given
// lock function. When elected, onElected is called with a context that remains
// valid for the duration of leadership. retryInterval controls how often a
// non-leader replica retries acquiring the lock.
func New(tryLock TryLockFunc, retryInterval time.Duration, onElected OnElected) *Elector {
	return &Elector{
		tryLock:       tryLock,
		retryInterval: retryInterval,
		onElected:     onElected,
	}
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
				e.relinquish()
				return
			case <-ticker.C:
				e.tryAcquire(ctx)
			}
		}
	}()
}

// Stop cancels the election loop and waits for it to finish.
// If this replica is the leader, it calls the stop function from OnElected.
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
}

// relinquish stops background workers if this replica is the leader.
// The advisory lock is automatically released when the Postgres connection
// is closed or the session ends.
func (e *Elector) relinquish() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.isLeader {
		return
	}

	slog.Info("leader: relinquishing leadership, stopping background workers")
	if e.stopFn != nil {
		e.stopFn()
		e.stopFn = nil
	}
	e.isLeader = false
}
