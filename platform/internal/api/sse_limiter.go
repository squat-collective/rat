package api

import (
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

// SSE connection limits to prevent DoS via long-lived streaming connections.
const (
	// MaxSSEDurationSeconds is the maximum lifetime of a single SSE connection (30 minutes).
	MaxSSEDurationSeconds = 30 * 60

	// MaxSSEPerIP is the maximum number of concurrent SSE connections from a single IP.
	MaxSSEPerIP = 10

	// MaxSSEGlobal is the global cap on concurrent SSE connections across all clients.
	MaxSSEGlobal = 1000
)

// SSELimiter tracks concurrent SSE connections per IP and globally.
// It uses atomic counters for the global cap and a mutex-protected map for per-IP tracking.
type SSELimiter struct {
	globalCount atomic.Int64
	mu          sync.Mutex
	perIP       map[string]*atomic.Int64
}

// NewSSELimiter creates a new SSE connection limiter.
func NewSSELimiter() *SSELimiter {
	return &SSELimiter{
		perIP: make(map[string]*atomic.Int64),
	}
}

// Acquire attempts to register a new SSE connection for the given IP.
// Returns true if the connection is allowed, false if any limit is exceeded.
// On success, the caller MUST call Release when the connection ends.
func (l *SSELimiter) Acquire(ip string) bool {
	// Check global limit first (cheap atomic check).
	if l.globalCount.Load() >= MaxSSEGlobal {
		return false
	}

	// Check per-IP limit.
	l.mu.Lock()
	counter, ok := l.perIP[ip]
	if !ok {
		counter = &atomic.Int64{}
		l.perIP[ip] = counter
	}
	l.mu.Unlock()

	if counter.Load() >= int64(MaxSSEPerIP) {
		return false
	}

	// Atomically increment both counters. Re-check limits after increment
	// to handle races (another goroutine may have incremented between check and add).
	ipCount := counter.Add(1)
	globalCount := l.globalCount.Add(1)

	if ipCount > int64(MaxSSEPerIP) || globalCount > MaxSSEGlobal {
		// Roll back — we exceeded the limit in a race.
		counter.Add(-1)
		l.globalCount.Add(-1)
		return false
	}

	return true
}

// Release decrements the connection counters for the given IP.
// Must be called exactly once for each successful Acquire.
func (l *SSELimiter) Release(ip string) {
	l.globalCount.Add(-1)

	l.mu.Lock()
	counter, ok := l.perIP[ip]
	l.mu.Unlock()

	if ok {
		if counter.Add(-1) <= 0 {
			// Clean up empty entries to avoid unbounded map growth.
			l.mu.Lock()
			if counter.Load() <= 0 {
				delete(l.perIP, ip)
			}
			l.mu.Unlock()
		}
	}
}

// GlobalCount returns the current global SSE connection count (for observability).
func (l *SSELimiter) GlobalCount() int64 {
	return l.globalCount.Load()
}

// IPCount returns the current SSE connection count for a specific IP.
func (l *SSELimiter) IPCount(ip string) int64 {
	l.mu.Lock()
	counter, ok := l.perIP[ip]
	l.mu.Unlock()

	if !ok {
		return 0
	}
	return counter.Load()
}

// clientIP extracts the client IP from the request, preferring X-Real-Ip
// (set by chi's RealIP middleware) and stripping the port from RemoteAddr.
func clientIP(r *http.Request) string {
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	// RemoteAddr is "host:port" — strip the port.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
