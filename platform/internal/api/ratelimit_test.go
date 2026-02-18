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

func TestRateLimit_UsesXRealIP(t *testing.T) {
	cfg := api.RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             1,
		CleanupInterval:   60_000_000_000,
	}

	rl, mw := api.RateLimit(cfg)
	defer rl.Stop()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request with X-Real-Ip header
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "proxy:1234"
	req.Header.Set("X-Real-Ip", "client-ip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Second request from same real IP — should be limited
	req = httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "proxy:1234"
	req.Header.Set("X-Real-Ip", "client-ip")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}
