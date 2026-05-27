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
