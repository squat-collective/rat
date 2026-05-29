package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/stretchr/testify/assert"
)

func TestRateLimit_AllowsBurst(t *testing.T) {
	cfg := api.RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             5,
		CleanupInterval:   60_000_000_000, // 1 minute
	}

	rl, mw := api.RateLimit(cfg)
	defer rl.Stop()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 5 requests should pass (burst)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should succeed", i+1)
	}

	// 6th request should be rate limited (burst exhausted)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestRateLimit_DifferentIPsAreIndependent(t *testing.T) {
	cfg := api.RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             2,
		CleanupInterval:   60_000_000_000,
	}

	rl, mw := api.RateLimit(cfg)
	defer rl.Stop()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust IP A's burst
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "1.1.1.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// IP B should still be allowed
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "2.2.2.2:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// The limiter keys on the resolved client IP (r.RemoteAddr, set upstream by
// realIPMiddleware) — NOT the raw X-Real-Ip header. A client must not be able to
// dodge or forge its rate-limit identity by setting that header itself.
func TestRateLimit_KeysOnRemoteAddr_IgnoresRawXRealIPHeader(t *testing.T) {
	cfg := api.RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             1,
		CleanupInterval:   60_000_000_000,
	}

	rl, mw := api.RateLimit(cfg)
	defer rl.Stop()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request from a given peer (with some X-Real-Ip header set).
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Real-Ip", "1.1.1.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Second request from the SAME peer but a DIFFERENT spoofed X-Real-Ip —
	// still limited, proving the header doesn't change the key.
	req = httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.5:9999"
	req.Header.Set("X-Real-Ip", "2.2.2.2")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}
