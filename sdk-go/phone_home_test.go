package sdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPhoneHome_SucceedsOnFirstAttempt(t *testing.T) {
	prev := PhoneHomeRetryDelay
	PhoneHomeRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { PhoneHomeRetryDelay = prev })

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/internal/plugins/register" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]string
		_ = json.Unmarshal(body, &got)
		if got["name"] != "myplugin" || got["addr"] != "myplugin:50099" {
			t.Errorf("payload = %v, want name=myplugin addr=myplugin:50099", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	err := PhoneHome(context.Background(), srv.URL, "myplugin", "myplugin:50099", 5)
	if err != nil {
		t.Fatalf("PhoneHome failed: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
}

func TestPhoneHome_RetriesOnTransientFailure(t *testing.T) {
	prev := PhoneHomeRetryDelay
	PhoneHomeRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { PhoneHomeRetryDelay = prev })

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Fail the first two attempts with 503, succeed on the third.
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	err := PhoneHome(context.Background(), srv.URL, "p", "p:1", 5)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 calls, got %d", got)
	}
}

func TestPhoneHome_GivesUpAfterMaxAttempts(t *testing.T) {
	prev := PhoneHomeRetryDelay
	PhoneHomeRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { PhoneHomeRetryDelay = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	err := PhoneHome(context.Background(), srv.URL, "p", "p:1", 3)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
}

func TestPhoneHome_RespectsContextCancellation(t *testing.T) {
	prev := PhoneHomeRetryDelay
	PhoneHomeRetryDelay = 50 * time.Millisecond
	t.Cleanup(func() { PhoneHomeRetryDelay = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := PhoneHome(ctx, srv.URL, "p", "p:1", 100)
	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
}

// ── Exponential backoff ──────────────────────────────────────────────

// TestBackoffForAttempt_ExponentialThenCapped pins the exact schedule
// the DoS fix promises: 0, init, 2·init, 4·init, … capped at MaxBackoff.
// This is the contract the SDK README documents — keep it green or
// update both at once.
func TestBackoffForAttempt_ExponentialThenCapped(t *testing.T) {
	opts := PhoneHomeOptions{
		MaxAttempts:    10,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
	}

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 0},                 // immediate
		{2, 1 * time.Second},   // +1s
		{3, 2 * time.Second},   // +2s
		{4, 4 * time.Second},   // +4s
		{5, 8 * time.Second},   // +8s
		{6, 16 * time.Second},  // +16s
		{7, 30 * time.Second},  // capped
		{8, 30 * time.Second},  // capped
		{9, 30 * time.Second},  // capped
		{10, 30 * time.Second}, // capped
	}
	for _, c := range cases {
		got := backoffForAttempt(opts, c.attempt)
		if got != c.want {
			t.Errorf("backoffForAttempt(attempt=%d) = %s, want %s", c.attempt, got, c.want)
		}
	}
}

// TestPhoneHomeWithOptions_StopsAtMaxAttempts proves the new options
// path bounds attempts the way DefaultPhoneHomeOptions promises.
func TestPhoneHomeWithOptions_StopsAtMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	opts := PhoneHomeOptions{
		MaxAttempts:    4,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     1 * time.Millisecond,
	}
	err := PhoneHomeWithOptions(context.Background(), srv.URL, "p", "p:1", opts)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if got := calls.Load(); got != 4 {
		t.Errorf("expected 4 attempts, got %d", got)
	}
}

// TestPhoneHomeOptions_ResolveAppliesDefaults locks the invalid-input
// handling: nonsense fields must fall back per-field rather than panic
// or silently freeze the loop.
func TestPhoneHomeOptions_ResolveAppliesDefaults(t *testing.T) {
	got := PhoneHomeOptions{}.resolve()
	want := DefaultPhoneHomeOptions()
	if got != want {
		t.Errorf("zero-value resolve() = %+v, want %+v", got, want)
	}

	// MaxBackoff below InitialBackoff is clamped up — otherwise the loop
	// would have negative or zero growth and never make progress.
	clamped := PhoneHomeOptions{MaxAttempts: 3, InitialBackoff: 5 * time.Second, MaxBackoff: 1 * time.Second}.resolve()
	if clamped.MaxBackoff != 5*time.Second {
		t.Errorf("MaxBackoff below Initial should clamp to Initial; got %s", clamped.MaxBackoff)
	}
}
