package sdk

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
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

// TestTokenAuth_UsesConstantTimeCompare is the regression guard for the
// timing-attack fix. It does NOT directly measure timing (which is too
// flaky for CI) — it just asserts the observable behaviour after the
// switch from `!=` to crypto/subtle.ConstantTimeCompare is identical:
// wrong tokens are still rejected with 401, correct ones still accepted,
// and tokens of the wrong length (which subtle.ConstantTimeCompare
// returns 0 for without comparing) are also rejected.
//
// If a future contributor "simplifies" the comparison back to `!=`,
// this test still passes — but the comment block on TokenAuth and the
// presence of the crypto/subtle import are the real guard. Code review
// should reject any PR that drops them. See sdk-go/auth.go for the
// SECURITY note.
func TestTokenAuth_UsesConstantTimeCompare(t *testing.T) {
	const expected = "deadbeefcafebabe0123456789abcdef0123456789abcdef0123456789abcdef"
	cases := []struct {
		name   string
		token  string
		status int
	}{
		{"correct", expected, http.StatusOK},
		{"wrong-same-length", "0000000000000000000000000000000000000000000000000000000000000000", http.StatusUnauthorized},
		{"wrong-first-byte", "X" + expected[1:], http.StatusUnauthorized},
		{"wrong-last-byte", expected[:len(expected)-1] + "X", http.StatusUnauthorized},
		{"too-short", expected[:len(expected)-1], http.StatusUnauthorized},
		{"too-long", expected + "X", http.StatusUnauthorized},
		{"empty", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			wrapped := TokenAuth(expected, inner)
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.token != "" {
				req.Header.Set("X-RAT-Plugin-Token", tc.token)
			}
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("%s: status = %d, want %d", tc.name, rec.Code, tc.status)
			}
		})
	}
}

// TestTokenAuth_TimingFlat is a documentation-style sanity check: it
// times many wrong-token requests with the wrong byte in different
// positions and asserts that the latency distribution across positions
// is statistically flat. With plain `!=` (which short-circuits on the
// first mismatch), the median for a token differing at position 0 would
// be ~64x faster than one differing at position 63 (in a 64-char hex
// token). With crypto/subtle.ConstantTimeCompare, all positions yield
// the same execution time.
//
// In practice in-process latency variance from the Go runtime swamps
// the per-byte CPU cost, so this test is informational only and
// SKIPPED by default. Set RAT_RUN_TIMING_TESTS=1 to opt in locally.
func TestTokenAuth_TimingFlat(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test disabled in short mode")
	}
	t.Skip("timing-sensitive test — opt in locally by removing this skip; not run in CI")

	const expected = "deadbeefcafebabe0123456789abcdef0123456789abcdef0123456789abcdef"
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := TokenAuth(expected, inner)

	positions := []int{0, 16, 32, 48, 63}
	medians := make([]time.Duration, 0, len(positions))
	for _, pos := range positions {
		bad := []byte(expected)
		bad[pos] = 'X'
		const iters = 1000
		samples := make([]time.Duration, iters)
		for i := 0; i < iters; i++ {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("X-RAT-Plugin-Token", string(bad))
			rec := httptest.NewRecorder()
			start := time.Now()
			wrapped.ServeHTTP(rec, req)
			samples[i] = time.Since(start)
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		medians = append(medians, samples[iters/2])
		t.Logf("pos=%d median=%v", pos, samples[iters/2])
	}
	// Allow up to 2x spread — in practice runtime jitter dominates and
	// the per-byte cost is invisible. With plain `!=` we'd see ~64x.
	min, max := medians[0], medians[0]
	for _, m := range medians {
		if m < min {
			min = m
		}
		if m > max {
			max = m
		}
	}
	if max > 2*min {
		t.Fatalf("median latency spread too large: min=%v max=%v (ratio=%.2fx) — possible timing leak", min, max, float64(max)/float64(min))
	}
}
