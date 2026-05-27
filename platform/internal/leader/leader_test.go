package leader

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock TryLockFunc helpers ---

// mockLock returns a TryLockFunc that can be dynamically controlled.
type mockLock struct {
	mu       sync.Mutex
	acquired bool
	err      error
	calls    int
}

func (m *mockLock) tryLock(_ context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.acquired, m.err
}

func (m *mockLock) setAcquired(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acquired = v
}

func (m *mockLock) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockPing is a controllable PingFunc that always returns the currently-set
// error (nil for success). Tests flip it mid-flight to simulate Postgres
// becoming unreachable.
type mockPing struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (p *mockPing) ping(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.err
}

func (p *mockPing) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *mockPing) getCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// mockUnlock records whether the unlock function was called and how many times.
type mockUnlock struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (u *mockUnlock) unlock(_ context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	return u.err
}

func (u *mockUnlock) getCalls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

// --- Tests ---

func TestElector_AcquiresLock_CallsOnElected(t *testing.T) {
	lock := &mockLock{acquired: true}
	var elected atomic.Bool

	elector := New(lock.tryLock, 50*time.Millisecond, func(_ context.Context) func() {
		elected.Store(true)
		return func() {}
	})

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// Give it time to acquire on the immediate first try.
	time.Sleep(30 * time.Millisecond)

	assert.True(t, elected.Load(), "onElected should have been called")
	assert.True(t, elector.IsLeader(), "should be leader after acquiring lock")

	cancel()
	elector.Stop()
}

func TestElector_LockNotAcquired_DoesNotCallOnElected(t *testing.T) {
	lock := &mockLock{acquired: false}
	var elected atomic.Bool

	elector := New(lock.tryLock, 50*time.Millisecond, func(_ context.Context) func() {
		elected.Store(true)
		return func() {}
	})

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// Wait enough for both the immediate try and one retry.
	time.Sleep(80 * time.Millisecond)

	assert.False(t, elected.Load(), "onElected should NOT be called when lock is not acquired")
	assert.False(t, elector.IsLeader(), "should not be leader")

	cancel()
	elector.Stop()
}

func TestElector_RetryAcquiresLock_EventuallyBecomesLeader(t *testing.T) {
	lock := &mockLock{acquired: false}
	var elected atomic.Bool

	elector := New(lock.tryLock, 50*time.Millisecond, func(_ context.Context) func() {
		elected.Store(true)
		return func() {}
	})

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// First attempt should fail (acquired=false).
	time.Sleep(30 * time.Millisecond)
	assert.False(t, elected.Load(), "should not be elected yet")

	// Make the lock available.
	lock.setAcquired(true)

	// Wait for a retry cycle.
	time.Sleep(80 * time.Millisecond)

	assert.True(t, elected.Load(), "should be elected after retry")
	assert.True(t, elector.IsLeader())

	cancel()
	elector.Stop()
}

func TestElector_DBError_DoesNotPanic(t *testing.T) {
	lock := &mockLock{err: fmt.Errorf("connection refused")}
	var elected atomic.Bool

	elector := New(lock.tryLock, 50*time.Millisecond, func(_ context.Context) func() {
		elected.Store(true)
		return func() {}
	})

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	time.Sleep(80 * time.Millisecond)

	assert.False(t, elected.Load(), "should not be elected when DB errors")
	assert.False(t, elector.IsLeader())
	assert.Greater(t, lock.getCalls(), 0, "should have attempted the query")

	cancel()
	elector.Stop()
}

func TestElector_Stop_CallsStopFn(t *testing.T) {
	lock := &mockLock{acquired: true}
	var stopped atomic.Bool

	elector := New(lock.tryLock, 50*time.Millisecond, func(_ context.Context) func() {
		return func() {
			stopped.Store(true)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// Wait for election.
	time.Sleep(30 * time.Millisecond)
	require.True(t, elector.IsLeader())

	cancel()
	elector.Stop()

	assert.True(t, stopped.Load(), "stop function from onElected should be called on shutdown")
	assert.False(t, elector.IsLeader(), "should no longer be leader after stop")
}

func TestElector_AlreadyLeader_DoesNotReElect(t *testing.T) {
	lock := &mockLock{acquired: true}
	var electCount atomic.Int32

	elector := New(lock.tryLock, 30*time.Millisecond, func(_ context.Context) func() {
		electCount.Add(1)
		return func() {}
	})

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// Wait for initial election + a few retry cycles.
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(1), electCount.Load(), "onElected should be called exactly once")

	cancel()
	elector.Stop()
}

func TestElector_IsLeader_DefaultFalse(t *testing.T) {
	lock := &mockLock{acquired: false}
	elector := New(lock.tryLock, time.Minute, func(_ context.Context) func() {
		return func() {}
	})

	assert.False(t, elector.IsLeader(), "should not be leader before Start()")
}

func TestAdvisoryLockID_IsStable(t *testing.T) {
	// The lock ID is a constant — ensure it doesn't accidentally change.
	assert.Equal(t, int64(7526700533049), AdvisoryLockID)
}

func TestElector_StopBeforeStart_DoesNotPanic(t *testing.T) {
	lock := &mockLock{acquired: false}
	elector := New(lock.tryLock, time.Minute, func(_ context.Context) func() {
		return func() {}
	})

	// Calling Stop without Start should not panic.
	elector.Stop()
}

// --- Heartbeat & voluntary release ---

// TestLeader_HeartbeatPing_Succeeds_LeadershipHeld asserts the happy path:
// the heartbeat keeps succeeding and the leader retains the lock.
func TestLeader_HeartbeatPing_Succeeds_LeadershipHeld(t *testing.T) {
	lock := &mockLock{acquired: true}
	ping := &mockPing{} // err=nil, always succeeds
	unlock := &mockUnlock{}
	var stoppedWorkers atomic.Bool

	elector := New(
		lock.tryLock,
		1*time.Hour, // disable the retry ticker
		func(_ context.Context) func() {
			return func() { stoppedWorkers.Store(true) }
		},
		WithPing(ping.ping),
		WithUnlock(unlock.unlock),
		WithHeartbeatInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// Wait long enough for several heartbeat ticks.
	time.Sleep(70 * time.Millisecond)

	assert.True(t, elector.IsLeader(), "should still be leader while heartbeat succeeds")
	assert.Greater(t, ping.getCalls(), 2, "heartbeat should have ticked multiple times")
	assert.Equal(t, 0, unlock.getCalls(), "should not have voluntarily unlocked")
	assert.False(t, stoppedWorkers.Load(), "workers should not have been stopped")

	cancel()
	elector.Stop()
}

// TestLeader_TwoConsecutiveHeartbeatFails_VoluntarilyReleases asserts that
// after the failure threshold is reached the leader unlocks and steps down.
func TestLeader_TwoConsecutiveHeartbeatFails_VoluntarilyReleases(t *testing.T) {
	lock := &mockLock{acquired: true}
	ping := &mockPing{} // starts healthy; we flip to failing after election
	unlock := &mockUnlock{}
	var stoppedWorkers atomic.Bool

	elector := New(
		lock.tryLock,
		1*time.Hour, // prevent re-acquisition during the test
		func(_ context.Context) func() {
			return func() { stoppedWorkers.Store(true) }
		},
		WithPing(ping.ping),
		WithUnlock(unlock.unlock),
		WithHeartbeatInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		elector.Stop()
	}()

	elector.Start(ctx)

	// Wait for election (and at least one healthy ping) before injecting
	// failures, so we know the heartbeat goroutine is actually running.
	require.Eventually(t, elector.IsLeader, 500*time.Millisecond, 5*time.Millisecond,
		"should become leader")
	require.Eventually(t, func() bool { return ping.getCalls() >= 1 }, 500*time.Millisecond, 5*time.Millisecond,
		"heartbeat goroutine should have ticked at least once")

	// Now inject the network partition.
	ping.setErr(fmt.Errorf("network partition: i/o timeout"))

	// Two consecutive failures need at least 2 heartbeat ticks (~20ms).
	// Give plenty of margin for the goroutine scheduler.
	require.Eventually(t, func() bool { return !elector.IsLeader() }, 500*time.Millisecond, 5*time.Millisecond,
		"leader should voluntarily step down after 2 consecutive heartbeat failures")

	assert.Equal(t, 1, unlock.getCalls(), "should have called pg_advisory_unlock exactly once")
	assert.True(t, stoppedWorkers.Load(), "background workers should have been stopped")
	assert.GreaterOrEqual(t, ping.getCalls(), heartbeatFailureThreshold,
		"ping should have been attempted at least the threshold count")
}

// TestLeader_HeartbeatRecovers_BeforeThreshold asserts that an intermittent
// failure (1 in a row, then success) does NOT trigger a step-down.
func TestLeader_HeartbeatRecovers_BeforeThreshold(t *testing.T) {
	lock := &mockLock{acquired: true}
	unlock := &mockUnlock{}

	// Fail-then-recover pattern: 1 failure, then success.
	var pingMu sync.Mutex
	var pingNum int
	pingFn := func(_ context.Context) error {
		pingMu.Lock()
		defer pingMu.Unlock()
		pingNum++
		if pingNum == 1 {
			return fmt.Errorf("transient error")
		}
		return nil
	}

	elector := New(
		lock.tryLock,
		1*time.Hour,
		func(_ context.Context) func() { return func() {} },
		WithPing(pingFn),
		WithUnlock(unlock.unlock),
		WithHeartbeatInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// Wait enough for multiple ticks.
	time.Sleep(80 * time.Millisecond)

	assert.True(t, elector.IsLeader(), "1 transient failure should not lose leadership")
	assert.Equal(t, 0, unlock.getCalls(), "should not have unlocked")

	cancel()
	elector.Stop()
}

// TestLeader_GracefulShutdown_UnlocksAdvisoryLock asserts that Stop() calls
// the unlock function (explicit release rather than relying on session death).
func TestLeader_GracefulShutdown_UnlocksAdvisoryLock(t *testing.T) {
	lock := &mockLock{acquired: true}
	ping := &mockPing{}
	unlock := &mockUnlock{}
	var stoppedWorkers atomic.Bool

	elector := New(
		lock.tryLock,
		1*time.Hour,
		func(_ context.Context) func() {
			return func() { stoppedWorkers.Store(true) }
		},
		WithPing(ping.ping),
		WithUnlock(unlock.unlock),
		WithHeartbeatInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	// Wait for the leader to be elected.
	require.Eventually(t, elector.IsLeader, 200*time.Millisecond, 5*time.Millisecond,
		"should become leader")

	// Graceful shutdown — context cancel triggers relinquish via the loop.
	cancel()
	elector.Stop()

	assert.Equal(t, 1, unlock.getCalls(), "graceful shutdown should explicit-unlock the advisory lock")
	assert.True(t, stoppedWorkers.Load(), "workers should be stopped on shutdown")
	assert.False(t, elector.IsLeader(), "should not be leader after shutdown")
}

// TestLeader_NoPingFunc_LegacyBehaviour_NoHeartbeat asserts that omitting
// WithPing preserves the old session-death-only behaviour (no heartbeat
// goroutine, no voluntary release). Backward compatibility.
func TestLeader_NoPingFunc_LegacyBehaviour_NoHeartbeat(t *testing.T) {
	lock := &mockLock{acquired: true}
	unlock := &mockUnlock{}

	elector := New(
		lock.tryLock,
		1*time.Hour,
		func(_ context.Context) func() { return func() {} },
		WithUnlock(unlock.unlock),
		// No WithPing — heartbeat should not run.
	)

	ctx, cancel := context.WithCancel(context.Background())
	elector.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	assert.True(t, elector.IsLeader(), "should be leader")

	cancel()
	elector.Stop()

	// Unlock is still called on graceful shutdown.
	assert.Equal(t, 1, unlock.getCalls(), "graceful shutdown still unlocks even without ping")
}
