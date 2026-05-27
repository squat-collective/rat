package sdk

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRandomToken_LengthAndUniqueness(t *testing.T) {
	a := RandomToken()
	b := RandomToken()
	// 32 raw bytes hex-encoded = 64 chars.
	if len(a) != 64 {
		t.Fatalf("RandomToken length = %d, want 64", len(a))
	}
	if len(b) != 64 {
		t.Fatalf("RandomToken length = %d, want 64", len(b))
	}
	if a == b {
		t.Fatal("two consecutive RandomToken() calls returned the same value — entropy broken")
	}
}

func TestSRIHash_KnownVector(t *testing.T) {
	// Known SRI for the empty string: sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=
	// (sha256("") in standard base64).
	got := SRIHash([]byte{})
	want := "sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
	if got != want {
		t.Fatalf("SRIHash(empty) = %q, want %q", got, want)
	}
}

func TestSRIHash_NonEmpty(t *testing.T) {
	got := SRIHash([]byte("hello"))
	if !strings.HasPrefix(got, "sha256-") {
		t.Fatalf("SRIHash should start with 'sha256-', got %q", got)
	}
	// Deterministic — calling twice yields the same value.
	if got != SRIHash([]byte("hello")) {
		t.Fatal("SRIHash is not deterministic")
	}
}

func TestTokenAuth_EmptyExpected_PassThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	wrapped := TokenAuth("", inner)
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty expected should pass through; got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want 'ok'", body)
	}
}

func TestTokenAuth_RejectsMissingHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called when token missing")
	})
	wrapped := TokenAuth("secret-token", inner)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token should yield 401, got %d", rec.Code)
	}
}

func TestTokenAuth_RejectsWrongHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called when token mismatches")
	})
	wrapped := TokenAuth("expected", inner)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-RAT-Plugin-Token", "wrong")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token should yield 401, got %d", rec.Code)
	}
}

func TestTokenAuth_AcceptsCorrectHeader(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := TokenAuth("expected", inner)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-RAT-Plugin-Token", "expected")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token should yield 200, got %d", rec.Code)
	}
	if !called {
		t.Fatal("inner handler was not invoked")
	}
}

func TestTokenAuth_AllowlistsHealth(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := TokenAuth("expected", inner)
	req := httptest.NewRequest(http.MethodGet, "/health", nil) // no token header
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/health should be allowed without token, got %d", rec.Code)
	}
	if !called {
		t.Fatal("/health did not reach inner handler")
	}
}

func TestTokenAuth_AllowlistsBundle(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := TokenAuth("expected", inner)
	req := httptest.NewRequest(http.MethodGet, "/bundle.js", nil) // no token header
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/bundle.js should be allowed without token, got %d", rec.Code)
	}
	if !called {
		t.Fatal("/bundle.js did not reach inner handler")
	}
}
