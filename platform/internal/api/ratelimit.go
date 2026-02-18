package api

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitConfig configures the per-IP rate limiter.
type RateLimitConfig struct {
	RequestsPerSecond float64       // Token refill rate (e.g. 50 = 50 req/s)
	Burst             int           // Max burst size (tokens in bucket)
	CleanupInterval   time.Duration // How often to evict stale entries
}

// DefaultRateLimitConfig returns sensible defaults (50 req/s, burst of 100).
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerSecond: 50,
		Burst:             100,
		CleanupInterval:   5 * time.Minute,
	}
}

// EndpointRateLimitConfig provides per-endpoint rate limit overrides.
// Endpoints not listed here use the global RateLimitConfig.
type EndpointRateLimitConfig struct {
	// Query endpoints get tighter limits because they execute SQL.
	Query RateLimitConfig
	// Preview endpoints get tight limits because they execute pipelines.
	Preview RateLimitConfig
	// Mutation endpoints (POST/PUT/DELETE) get moderate limits.
	Mutation RateLimitConfig
}

// DefaultEndpointRateLimitConfig returns per-endpoint defaults.
func DefaultEndpointRateLimitConfig() EndpointRateLimitConfig {
	return EndpointRateLimitConfig{
		Query: RateLimitConfig{
			RequestsPerSecond: 10,
			Burst:             20,
			CleanupInterval:   5 * time.Minute,
		},
		Preview: RateLimitConfig{
			RequestsPerSecond: 5,
			Burst:             10,
			CleanupInterval:   5 * time.Minute,
		},
		Mutation: RateLimitConfig{
			RequestsPerSecond: 20,
			Burst:             40,
			CleanupInterval:   5 * time.Minute,
		},
	}
}

// RateLimitForEndpoint creates a rate limit middleware for a specific endpoint type.
// Use this for endpoints that need tighter limits than the global default.
func RateLimitForEndpoint(cfg RateLimitConfig) (*RateLimiter, func(http.Handler) http.Handler) {
	return RateLimit(cfg)
}

// tokenBucket implements a simple per-IP token bucket.
type tokenBucket struct {
	tokens   float64
	maxBurst float64
	rate     float64 // tokens per second
	lastSeen time.Time
}

func (b *tokenBucket) allow(now time.Time) bool {
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.maxBurst {
		b.tokens = b.maxBurst
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimiter is a concurrent-safe per-IP rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	config  RateLimitConfig
	stop    chan struct{}
}

// newRateLimiter creates a rate limiter and starts background cleanup.
func newRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		config:  cfg,
		stop:    make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// rateLimitResult holds the outcome of a rate limit check including
// remaining tokens for inclusion in response headers.
type rateLimitResult struct {
	Allowed   bool
	Remaining int     // approximate tokens remaining (for RateLimit-Remaining header)
	ResetMs   int64   // milliseconds until a token is available (for Retry-After)
	Limit     int     // bucket capacity (for RateLimit-Limit header)
}

// allow checks whether a request from the given IP is allowed.
func (rl *RateLimiter) allow(ip string) rateLimitResult {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &tokenBucket{
			tokens:   float64(rl.config.Burst),
			maxBurst: float64(rl.config.Burst),
			rate:     rl.config.RequestsPerSecond,
			lastSeen: now,
		}
		rl.buckets[ip] = b
	}

	allowed := b.allow(now)
	remaining := int(math.Max(0, b.tokens))
	var resetMs int64
	if !allowed && b.rate > 0 {
		// Time until next token becomes available.
		resetMs = int64((1.0 - b.tokens) / b.rate * 1000)
		if resetMs < 0 {
			resetMs = 1000 // minimum 1 second
		}
	}

	return rateLimitResult{
		Allowed:   allowed,
		Remaining: remaining,
		ResetMs:   resetMs,
		Limit:     int(b.maxBurst),
	}
}

// cleanup periodically removes stale IP entries (no requests for 10+ minutes).
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for ip, b := range rl.buckets {
				if b.lastSeen.Before(cutoff) {
					delete(rl.buckets, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Stop gracefully shuts down the rate limiter's background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	select {
	case <-rl.stop:
		// already closed
	default:
		close(rl.stop)
	}
}

// setRateLimitHeaders adds standard rate limit headers to the response.
// These headers follow the IETF RateLimit header fields draft:
// - RateLimit-Limit: maximum requests per window
// - RateLimit-Remaining: remaining requests in current window
// - Retry-After: seconds until next request allowed (only on 429)
func setRateLimitHeaders(w http.ResponseWriter, result rateLimitResult) {
	w.Header().Set("RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("RateLimit-Remaining", strconv.Itoa(result.Remaining))
	if !result.Allowed {
		retryAfterSecs := (result.ResetMs + 999) / 1000 // round up to seconds
		if retryAfterSecs < 1 {
			retryAfterSecs = 1
		}
		w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSecs, 10))
	}
}

// RateLimit returns a middleware that limits requests per IP.
// The returned RateLimiter can be stopped via its Stop() method.
// On 429 responses, standard rate limit headers are included.
func RateLimit(cfg RateLimitConfig) (*RateLimiter, func(http.Handler) http.Handler) {
	rl := newRateLimiter(cfg)

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			// chi's RealIP middleware sets X-Real-IP
			if xri := r.Header.Get("X-Real-Ip"); xri != "" {
				ip = xri
			}

			result := rl.allow(ip)
			setRateLimitHeaders(w, result)

			if !result.Allowed {
				errorJSON(w, "rate limit exceeded", "RESOURCE_EXHAUSTED", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	return rl, mw
}
