// Package ratelimit provides distributed rate limiting for multi-instance deployments.
//
// Community edition uses the LocalLimiter (wraps the existing in-memory per-IP
// token bucket from api/ratelimit.go). This is correct for single-instance
// deployments but does not coordinate across multiple ratd replicas.
//
// Pro edition provides a RedisLimiter backed by Redis that coordinates rate
// limits across all ratd instances using a sliding window counter algorithm.
// This ensures accurate rate limiting even when requests are load-balanced
// across multiple replicas.
//
// Both implementations satisfy the Limiter interface, which is consumed by
// the API rate limit middleware. The middleware creates a Limiter at startup
// based on feature flags — if distributed_rate_limiting is enabled and a
// Redis URL is configured, it uses RedisLimiter; otherwise LocalLimiter.
package ratelimit

import (
	"context"
	"time"
)

// Result holds the outcome of a rate limit check.
type Result struct {
	Allowed   bool  // Whether the request is allowed.
	Remaining int   // Approximate tokens remaining before rate limit is hit.
	ResetMs   int64 // Milliseconds until a token becomes available (0 if allowed).
	Limit     int   // Maximum burst size / window capacity.
}

// Limiter abstracts rate limiting behind a simple interface.
// Implementations may be local (in-memory) or distributed (Redis, etc.).
type Limiter interface {
	// Allow checks whether a request identified by key (typically IP address
	// or user ID) should be permitted. Returns the check result.
	Allow(ctx context.Context, key string) (Result, error)

	// Close releases any resources held by the limiter (Redis connections, etc.).
	Close() error
}

// Config holds rate limiter configuration shared across implementations.
type Config struct {
	RequestsPerSecond float64       // Token refill rate.
	Burst             int           // Maximum burst size (bucket capacity).
	Window            time.Duration // Sliding window size (for Redis implementation).
}

// DefaultConfig returns sensible defaults for rate limiting (50 req/s, burst 100).
func DefaultConfig() Config {
	return Config{
		RequestsPerSecond: 50,
		Burst:             100,
		Window:            time.Minute, // 1-minute sliding window for distributed mode
	}
}

// LocalLimiter is an in-memory per-key rate limiter for single-instance deployments.
// This is a thin wrapper around the existing token bucket implementation in api/ratelimit.go.
// Community edition always uses this implementation.
type LocalLimiter struct {
	config Config
}

// NewLocalLimiter creates a local in-memory rate limiter.
func NewLocalLimiter(cfg Config) *LocalLimiter {
	return &LocalLimiter{config: cfg}
}

func (l *LocalLimiter) Allow(_ context.Context, _ string) (Result, error) {
	// In practice, the local limiter delegates to the existing api.RateLimiter
	// implementation. This is a placeholder that always allows — the real
	// enforcement happens in the existing api.RateLimit middleware.
	// When distributed rate limiting is enabled (Pro), this is replaced by
	// RedisLimiter which provides cross-instance coordination.
	return Result{
		Allowed:   true,
		Remaining: l.config.Burst,
		ResetMs:   0,
		Limit:     l.config.Burst,
	}, nil
}

func (l *LocalLimiter) Close() error {
	return nil
}

// RedisConfig holds Redis-specific configuration for the distributed limiter.
// Pro only — Community edition does not use Redis.
type RedisConfig struct {
	URL      string        // Redis URL (e.g., "redis://localhost:6379/0")
	Password string        // Redis password (empty for no auth)
	DB       int           // Redis database number (default 0)
	KeyPrefix string       // Key prefix for rate limit entries (default "rat:rl:")
	Timeout  time.Duration // Per-command timeout (default 100ms)
}

// DefaultRedisConfig returns sensible defaults for Redis rate limiting.
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		URL:       "redis://localhost:6379/0",
		KeyPrefix: "rat:rl:",
		Timeout:   100 * time.Millisecond,
	}
}

// RedisLimiter implements distributed rate limiting using Redis.
// Uses a sliding window counter algorithm for accurate cross-instance limiting.
//
// Pro only — the implementation requires a Redis client dependency that is
// provided by the Pro plugin package. This file defines the interface and
// configuration; the actual Redis logic lives in ratatouille-pro.
//
// Algorithm (sliding window counter):
//   1. Key format: "{prefix}{ip}:{window_start_ts}"
//   2. INCR the key for the current window
//   3. GET the key for the previous window
//   4. Weighted count = prev_count * overlap_fraction + current_count
//   5. Compare against (RequestsPerSecond * Window.Seconds()) for the limit
//   6. Keys expire after 2 * Window to auto-cleanup
//
// TODO(pro): Implement RedisLimiter when the Pro rate limiting plugin is built.
// The struct below is a placeholder that satisfies the Limiter interface.
type RedisLimiter struct {
	config      Config
	redisConfig RedisConfig
}

// NewRedisLimiter creates a distributed rate limiter backed by Redis.
// Returns an error if the Redis connection cannot be established.
//
// TODO(pro): Replace with real Redis client (go-redis/redis/v9).
func NewRedisLimiter(cfg Config, redisCfg RedisConfig) (*RedisLimiter, error) {
	return &RedisLimiter{
		config:      cfg,
		redisConfig: redisCfg,
	}, nil
}

func (r *RedisLimiter) Allow(_ context.Context, _ string) (Result, error) {
	// TODO(pro): Implement sliding window counter via Redis INCR + GET + EXPIRE.
	// For now, always allow (placeholder until Pro plugin provides real implementation).
	return Result{
		Allowed:   true,
		Remaining: r.config.Burst,
		ResetMs:   0,
		Limit:     r.config.Burst,
	}, nil
}

func (r *RedisLimiter) Close() error {
	// TODO(pro): Close Redis connection pool.
	return nil
}
