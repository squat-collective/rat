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
