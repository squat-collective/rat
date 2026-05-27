package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unit tests for the per-name token-bucket limiter. The handler-level tests
// in plugins_test.go cover the wire contract (429 + Retry-After); these
// pin the limiter's clock-driven behaviour without sleeping.

func TestRegisterLimiter_FreshNameStartsFull(t *testing.T) {
	rl := newRegisterLimiter(registerLimiterConfig{
		RatePerMinute: 10,
		Burst:         10,
		IdleTTL:       10 * time.Minute,
	})

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A brand-new name should fire `Burst` calls back-to-back before being denied.
	for i := 0; i < 10; i++ {
		require.True(t, rl.allow("p", now).Allowed, "burst slot %d", i+1)
	}
	res := rl.allow("p", now)
	assert.False(t, res.Allowed, "11th immediate call must be denied")
	assert.GreaterOrEqual(t, res.RetryAfterSecs, int64(1),
		"Retry-After must be at least 1 second so the client never busy-loops")
}

func TestRegisterLimiter_RefillsOverTime(t *testing.T) {
	rl := newRegisterLimiter(registerLimiterConfig{
		RatePerMinute: 10, // 1 token per 6s
		Burst:         10,
		IdleTTL:       10 * time.Minute,
	})

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Drain the bucket.
	for i := 0; i < 10; i++ {
		require.True(t, rl.allow("p", start).Allowed)
	}
	require.False(t, rl.allow("p", start).Allowed, "drained bucket should deny")

	// After 6 seconds we earn exactly 1 token back.
	require.True(t, rl.allow("p", start.Add(6*time.Second)).Allowed,
		"one full refill interval should grant one more call")
	require.False(t, rl.allow("p", start.Add(6*time.Second)).Allowed,
		"and only one — the bucket is empty again")
}

func TestRegisterLimiter_PerNameIsolation(t *testing.T) {
	rl := newRegisterLimiter(registerLimiterConfig{
		RatePerMinute: 10,
		Burst:         10,
		IdleTTL:       10 * time.Minute,
	})

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		require.True(t, rl.allow("a", now).Allowed)
	}
	require.False(t, rl.allow("a", now).Allowed)
	// `b` is untouched — must still have its full burst.
	require.True(t, rl.allow("b", now).Allowed,
		"throttling one name must not affect another's bucket")
}

func TestRegisterLimiter_GCsIdleBuckets(t *testing.T) {
	rl := newRegisterLimiter(registerLimiterConfig{
		RatePerMinute: 10,
		Burst:         10,
		IdleTTL:       1 * time.Minute,
	})

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rl.allow("ephemeral", now) // creates the bucket
	require.Equal(t, 1, rl.size())

	// Touch the limiter with a different name well after IdleTTL — the lazy
	// sweep should fire and evict the stale bucket.
	future := now.Add(2 * time.Minute)
	rl.allow("active", future)
	assert.Equal(t, 1, rl.size(),
		"sweep should drop the idle bucket; only the active name remains")
}

func TestNewRegisterLimiter_AppliesDefaultsForZeroFields(t *testing.T) {
	// Zero/negative config fields would otherwise produce a 0-rate, 0-burst
	// limiter that denies every call — guard against accidental misuse.
	rl := newRegisterLimiter(registerLimiterConfig{})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		require.True(t, rl.allow("p", now).Allowed,
			"default Burst should be at least 10 (matches public contract)")
	}
}
