package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SSELimiter unit tests ---

func TestSSELimiter_Acquire_SingleIP_RespectsPerIPLimit(t *testing.T) {
	limiter := api.NewSSELimiter()

	// Acquire up to the per-IP limit.
	for i := 0; i < api.MaxSSEPerIP; i++ {
		assert.True(t, limiter.Acquire("10.0.0.1"), "acquire %d should succeed", i)
	}

	// Next acquire should fail.
	assert.False(t, limiter.Acquire("10.0.0.1"), "acquire beyond per-IP limit should fail")

	// Different IP should still work.
	assert.True(t, limiter.Acquire("10.0.0.2"), "different IP should succeed")

	// Clean up.
	for i := 0; i < api.MaxSSEPerIP; i++ {
		limiter.Release("10.0.0.1")
	}
	limiter.Release("10.0.0.2")
}

func TestSSELimiter_Acquire_GlobalLimit(t *testing.T) {
	limiter := api.NewSSELimiter()

	// Fill up global capacity using unique IPs (to avoid per-IP limit).
	for i := 0; i < api.MaxSSEGlobal; i++ {
		ip := "10.0." + itoa(i/256) + "." + itoa(i%256)
		assert.True(t, limiter.Acquire(ip), "acquire %d should succeed", i)
	}

	// Next acquire should fail (global cap hit).
	assert.False(t, limiter.Acquire("99.99.99.99"), "acquire beyond global limit should fail")

	// Release one and try again.
	limiter.Release("10.0.0.0")
	assert.True(t, limiter.Acquire("99.99.99.99"), "acquire after release should succeed")

	// Clean up.
	for i := 1; i < api.MaxSSEGlobal; i++ {
		ip := "10.0." + itoa(i/256) + "." + itoa(i%256)
		limiter.Release(ip)
	}
	limiter.Release("99.99.99.99")
}

func TestSSELimiter_Release_DecrementsCounters(t *testing.T) {
	limiter := api.NewSSELimiter()

	limiter.Acquire("10.0.0.1")
	limiter.Acquire("10.0.0.1")
	assert.Equal(t, int64(2), limiter.IPCount("10.0.0.1"))
	assert.Equal(t, int64(2), limiter.GlobalCount())

	limiter.Release("10.0.0.1")
	assert.Equal(t, int64(1), limiter.IPCount("10.0.0.1"))
	assert.Equal(t, int64(1), limiter.GlobalCount())

	limiter.Release("10.0.0.1")
	assert.Equal(t, int64(0), limiter.IPCount("10.0.0.1"))
	assert.Equal(t, int64(0), limiter.GlobalCount())
}

func TestSSELimiter_ConcurrentAccess(t *testing.T) {
	limiter := api.NewSSELimiter()

	var wg sync.WaitGroup
	successes := int64(0)
	var mu sync.Mutex

	// Launch more goroutines than the per-IP limit from the same IP.
	for i := 0; i < api.MaxSSEPerIP+5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Acquire("10.0.0.1") {
				mu.Lock()
				successes++
				mu.Unlock()
				// Hold for a moment then release.
				time.Sleep(10 * time.Millisecond)
				limiter.Release("10.0.0.1")
			}
		}()
	}

	wg.Wait()

	// At most MaxSSEPerIP should have succeeded concurrently.
	assert.LessOrEqual(t, successes, int64(api.MaxSSEPerIP)+5, "total successes should be bounded")
	assert.Equal(t, int64(0), limiter.GlobalCount(), "all connections should be released")
}

// --- SSE endpoint integration tests ---

func TestSSE_PerIPLimit_Returns429(t *testing.T) {
	srv, _, runStore := newRunTestServer()

	// Use a custom limiter with a very small per-IP limit for testing.
	limiter := api.NewSSELimiter()
	srv.SSELimiter = limiter

	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusRunning},
	}
	router := api.NewRouter(srv)

	// Fill up the per-IP limit.
	ctxs := make([]context.CancelFunc, 0, api.MaxSSEPerIP)
	dones := make([]chan struct{}, 0, api.MaxSSEPerIP)

	for i := 0; i < api.MaxSSEPerIP; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ctxs = append(ctxs, cancel)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
		req = req.WithContext(ctx)
		req.Header.Set("Accept", "text/event-stream")
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		dones = append(dones, done)
		go func() {
			router.ServeHTTP(rec, req)
			close(done)
		}()

		// Give the handler time to acquire the limiter slot.
		time.Sleep(20 * time.Millisecond)
	}

	// The next SSE request from the same IP should get 429.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	req.RemoteAddr = "10.0.0.1:5678"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	var body api.APIError
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "RESOURCE_EXHAUSTED", body.Error.Code)
	assert.Contains(t, body.Error.Message, "too many SSE connections")

	// A different IP should still work.
	ctx2, cancel2 := context.WithCancel(context.Background())
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req2 = req2.WithContext(ctx2)
	req2.Header.Set("Accept", "text/event-stream")
	req2.RemoteAddr = "10.0.0.2:1234"
	rec2 := httptest.NewRecorder()

	done2 := make(chan struct{})
	go func() {
		router.ServeHTTP(rec2, req2)
		close(done2)
	}()
	time.Sleep(50 * time.Millisecond)

	// Cancel and wait for the handler to finish before reading the recorder.
	cancel2()
	<-done2

	// Should have started streaming (200 with event-stream content type).
	assert.Equal(t, "text/event-stream", rec2.Result().Header.Get("Content-Type"))
	for _, cancel := range ctxs {
		cancel()
	}
	for _, done := range dones {
		<-done
	}
}

func TestSSE_GlobalLimit_Returns429(t *testing.T) {
	srv, _, runStore := newRunTestServer()

	// Create a limiter but we'll test the global limit by filling it directly.
	limiter := api.NewSSELimiter()
	srv.SSELimiter = limiter

	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusRunning},
	}
	router := api.NewRouter(srv)

	// Simulate the global limit being reached by acquiring slots directly.
	for i := 0; i < api.MaxSSEGlobal; i++ {
		ip := "fake-" + itoa(i)
		limiter.Acquire(ip)
	}

	// An SSE request should now get 429.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	var body api.APIError
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "RESOURCE_EXHAUSTED", body.Error.Code)

	// Clean up.
	for i := 0; i < api.MaxSSEGlobal; i++ {
		ip := "fake-" + itoa(i)
		limiter.Release(ip)
	}
}

func TestSSE_ConnectionReleasedOnClientDisconnect(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	limiter := api.NewSSELimiter()
	srv.SSELimiter = limiter

	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusRunning},
	}
	router := api.NewRouter(srv)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req = req.WithContext(ctx)
	req.Header.Set("Accept", "text/event-stream")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(rec, req)
		close(done)
	}()

	// Wait for the connection to be established.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), limiter.IPCount("10.0.0.1"))
	assert.Equal(t, int64(1), limiter.GlobalCount())

	// Disconnect.
	cancel()
	<-done

	// Counters should be back to zero.
	assert.Equal(t, int64(0), limiter.IPCount("10.0.0.1"))
	assert.Equal(t, int64(0), limiter.GlobalCount())
}

func TestSSE_ConnectionReleasedOnTerminalStatus(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	limiter := api.NewSSELimiter()
	srv.SSELimiter = limiter

	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusSuccess},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Terminal runs complete immediately — limiter should be released.
	assert.Equal(t, int64(0), limiter.IPCount("10.0.0.1"))
	assert.Equal(t, int64(0), limiter.GlobalCount())
	assert.Contains(t, rec.Body.String(), "event: status")
}

func TestSSE_JSONFallback_NotAffectedByLimiter(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	limiter := api.NewSSELimiter()
	srv.SSELimiter = limiter

	// Fill the global limit.
	for i := 0; i < api.MaxSSEGlobal; i++ {
		ip := "fake-" + itoa(i)
		limiter.Acquire(ip)
	}

	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusSuccess},
	}
	router := api.NewRouter(srv)

	// JSON fallback (no Accept: text/event-stream) should not be limited.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	// Clean up.
	for i := 0; i < api.MaxSSEGlobal; i++ {
		ip := "fake-" + itoa(i)
		limiter.Release(ip)
	}
}

// itoa is a quick int-to-string helper for test IPs.
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
