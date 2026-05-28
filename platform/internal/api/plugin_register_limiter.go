package api

import (
	"sync"
	"time"
)

// registerLimiterConfig configures the per-plugin-name token bucket for
// /internal/plugins/register. See pluginRegisterLimiter in plugins.go for the
// production instance.
type registerLimiterConfig struct {
	// RatePerMinute is the steady-state refill rate per name (tokens / minute).
	RatePerMinute float64
	// Burst is the bucket capacity. Initial tokens == Burst, so a fresh name
	// can fire `Burst` register calls in a tight loop before being throttled.
	Burst int
	// IdleTTL governs lazy GC: buckets not touched within this window are
	// swept the next time any name takes the limiter lock, so memory stays
	// bounded even under high churn (e.g. operators reshuffling plugin names).
	IdleTTL time.Duration
}

// registerBucket is a single per-name token bucket. Mutated only under the
// owning registerLimiter's mu, so no field-level locking is needed.
type registerBucket struct {
	tokens   float64
	lastSeen time.Time
}

// registerLimiter is the per-plugin-name token-bucket rate limiter used by
// HandlePluginRegister. Concurrent-safe; intended for module-scope use so the
// bucket state survives across handler invocations.
type registerLimiter struct {
	cfg registerLimiterConfig

	// ratePerSec is RatePerMinute/60 cached so allow() doesn't recompute.
	ratePerSec float64
	burst      float64

	mu      sync.Mutex
	buckets map[string]*registerBucket
	// nextSweep is the wall-clock time after which the next register call may
	// trigger a lazy GC scan. We rate-limit the sweep itself to once per
	// IdleTTL so that a hot path full of registers doesn't pay O(n) every
	// time.
	nextSweep time.Time
}

// registerLimitResult is the outcome of an allow() check. RetryAfterSecs is
// only meaningful when Allowed is false; it is rounded up to the nearest
// second and is at least 1 so a 429 client never busy-loops.
type registerLimitResult struct {
	Allowed        bool
	RetryAfterSecs int64
}

// newRegisterLimiter constructs a limiter. Zero/negative config fields fall
// back to sensible defaults so callers can't accidentally produce an
// always-deny or always-allow limiter.
func newRegisterLimiter(cfg registerLimiterConfig) *registerLimiter {
	if cfg.RatePerMinute <= 0 {
		cfg.RatePerMinute = 10
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 10
	}
	if cfg.IdleTTL <= 0 {
		cfg.IdleTTL = 10 * time.Minute
	}
	return &registerLimiter{
		cfg:        cfg,
		ratePerSec: cfg.RatePerMinute / 60.0,
		burst:      float64(cfg.Burst),
		buckets:    make(map[string]*registerBucket),
	}
}

// allow charges one token from `name`'s bucket. now is injectable so tests
// can simulate the passage of time without sleeping.
//
// On the first call for a given name, the bucket is created full (so a
// healthy boot phone-home that succeeds on attempt 1 never sees a 429).
func (l *registerLimiter) allow(name string, now time.Time) registerLimitResult {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Lazy GC: at most once per IdleTTL, sweep buckets older than IdleTTL.
	// Cheap because IdleTTL is large relative to register frequency, and
	// avoiding a goroutine keeps tests deterministic.
	if now.After(l.nextSweep) {
		cutoff := now.Add(-l.cfg.IdleTTL)
		for k, b := range l.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
		l.nextSweep = now.Add(l.cfg.IdleTTL)
	}

	b, ok := l.buckets[name]
	if !ok {
		b = &registerBucket{tokens: l.burst, lastSeen: now}
		l.buckets[name] = b
	} else {
		elapsed := now.Sub(b.lastSeen).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.ratePerSec
			if b.tokens > l.burst {
				b.tokens = l.burst
			}
		}
		b.lastSeen = now
	}

	if b.tokens < 1 {
		// Time until the next whole token is available, in seconds, rounded
		// up so a client that honours Retry-After never retries inside the
		// same window we just denied them.
		need := 1.0 - b.tokens
		secs := int64(need / l.ratePerSec)
		if float64(secs)*l.ratePerSec < need {
			secs++
		}
		if secs < 1 {
			secs = 1
		}
		return registerLimitResult{Allowed: false, RetryAfterSecs: secs}
	}
	b.tokens--
	return registerLimitResult{Allowed: true}
}

// size is a test helper: how many distinct names currently hold buckets.
func (l *registerLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
