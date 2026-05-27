package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// PhoneHomeRetryDelay was the historical FIXED pause between phone-home
// attempts. Kept as a package var only because external tests imported it;
// the new exponential-backoff path reads PhoneHomeOptions instead.
//
// Deprecated: use PhoneHomeOptions.InitialBackoff.
var PhoneHomeRetryDelay = 1 * time.Second

// PhoneHomeOptions tunes the boot-time retry schedule. The defaults
// (DefaultPhoneHomeOptions) produce ~10 attempts spread across ~3 minutes
// with exponential backoff capped at 30s; tests and special-case callers
// can override.
type PhoneHomeOptions struct {
	// MaxAttempts is the total number of register attempts including the
	// first. Must be >= 1; values <1 fall back to the default.
	MaxAttempts int
	// InitialBackoff is the pause before attempt #2 (attempt #1 fires
	// immediately). Doubles each attempt until MaxBackoff. Must be > 0;
	// values <=0 fall back to the default.
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential growth. Must be >= InitialBackoff;
	// otherwise it is silently clamped up to InitialBackoff.
	MaxBackoff time.Duration
}

// DefaultPhoneHomeOptions is the schedule used by PhoneHomeLoop. The
// schedule favours a slow ramp so a crashlooping or compromised plugin
// cannot hammer ratd's internal listener:
//
//	attempt 1 → immediate
//	attempt 2 → +1s
//	attempt 3 → +2s
//	attempt 4 → +4s
//	attempt 5 → +8s
//	attempt 6 → +16s
//	attempts 7-10 → +30s each (capped)
//	total wall-clock ≈ 3 minutes for 10 attempts.
//
// Previously this was 30 attempts × 2s fixed = 60s of constant pressure
// on /internal/plugins/register; now ratd sees at most one register burst
// per ~5 minutes from a wedged plugin.
func DefaultPhoneHomeOptions() PhoneHomeOptions {
	return PhoneHomeOptions{
		MaxAttempts:    10,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
	}
}

// resolve normalises options: invalid fields fall back to the matching
// default-value field. Returned struct is safe to consume directly.
//
// Note on MaxBackoff: zero/negative falls back to the default (30s),
// then any positive-but-below-Initial value is clamped UP to Initial so
// the exponential ramp can never go backwards (which would freeze the
// loop into a no-progress state).
func (o PhoneHomeOptions) resolve() PhoneHomeOptions {
	def := DefaultPhoneHomeOptions()
	if o.MaxAttempts < 1 {
		o.MaxAttempts = def.MaxAttempts
	}
	if o.InitialBackoff <= 0 {
		o.InitialBackoff = def.InitialBackoff
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = def.MaxBackoff
	}
	if o.MaxBackoff < o.InitialBackoff {
		o.MaxBackoff = o.InitialBackoff
	}
	return o
}

// backoffForAttempt returns the wait before `attempt` (1-indexed).
// Attempt 1 returns 0 (fire immediately); attempt 2 returns Initial;
// attempt 3 returns Initial*2; capped at MaxBackoff.
//
// Exposed for tests; not part of the SDK public API contract.
func backoffForAttempt(opts PhoneHomeOptions, attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	// attempt=2 → multiplier 1, attempt=3 → multiplier 2, ...
	// Cap at MaxBackoff/InitialBackoff to avoid overflow on huge attempt counts.
	wait := opts.InitialBackoff
	for i := 2; i < attempt; i++ {
		next := wait * 2
		if next <= 0 || next > opts.MaxBackoff {
			return opts.MaxBackoff
		}
		wait = next
	}
	if wait > opts.MaxBackoff {
		return opts.MaxBackoff
	}
	return wait
}

// PhoneHome registers the plugin with ratd's internal listener. It
// retries up to maxAttempts times with PhoneHomeRetryDelay between
// attempts. Returns nil on success or the last error after exhaustion.
//
// Honours ctx for cancellation between attempts. Each attempt uses its
// own 10s timeout so a stuck ratd can't pin the registration loop.
//
// This thin compatibility wrapper preserves the old (maxAttempts int)
// signature while delegating to the exponential-backoff implementation.
// New callers should prefer PhoneHomeWithOptions for control over the
// schedule.
func PhoneHome(ctx context.Context, ratdInternalURL, name, addr string, maxAttempts int) error {
	opts := DefaultPhoneHomeOptions()
	opts.MaxAttempts = maxAttempts
	// Preserve the historical override hook for tests that still set
	// PhoneHomeRetryDelay before calling PhoneHome (no exponential growth
	// in that legacy path — the test wants a fast, fixed cadence).
	opts.InitialBackoff = PhoneHomeRetryDelay
	opts.MaxBackoff = PhoneHomeRetryDelay
	return phoneHome(ctx, ratdInternalURL, name, addr, opts)
}

// PhoneHomeWithOptions is the modern entry-point: control the full retry
// schedule (max attempts, initial backoff, cap) so tests and special
// callers can substitute a tighter or looser schedule. Invalid options
// fall back to DefaultPhoneHomeOptions field-by-field.
func PhoneHomeWithOptions(ctx context.Context, ratdInternalURL, name, addr string, opts PhoneHomeOptions) error {
	return phoneHome(ctx, ratdInternalURL, name, addr, opts.resolve())
}

// phoneHome is the shared core. opts are assumed already resolved.
//
// Endpoint shape: /api/v1/internal/plugins/register is the canonical
// post-harmonisation URL (every internal route under /api/v1/internal/*).
// ratd also serves the legacy /internal/plugins/register as a deprecated
// alias for one release cycle, so older SDK builds keep working — but new
// SDK consumers write here. See ADR-019 + internal_routes.go in ratd for
// the trust model and the deprecation contract.
func phoneHome(ctx context.Context, ratdInternalURL, name, addr string, opts PhoneHomeOptions) error {
	endpoint := ratdInternalURL + "/api/v1/internal/plugins/register"
	body, err := json.Marshal(map[string]string{"name": name, "addr": addr})
	if err != nil {
		return fmt.Errorf("marshal register body: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		wait := backoffForAttempt(opts, attempt)
		if wait > 0 {
			select {
			case <-ctx.Done():
				if lastErr == nil {
					return ctx.Err()
				}
				return fmt.Errorf("phone-home cancelled after %d attempts: %w (last attempt: %v)", attempt-1, ctx.Err(), lastErr)
			case <-time.After(wait):
			}
		} else if err := ctx.Err(); err != nil {
			// Fast path: respect a context that's already cancelled before
			// we even fire attempt #1.
			return err
		}

		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			// Building a request shouldn't fail with a static endpoint
			// + body; if it does, the env is structurally broken.
			return fmt.Errorf("build register request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("ratd returned status %d", resp.StatusCode)
	}
	if lastErr == nil {
		lastErr = errors.New("unknown error")
	}
	return fmt.Errorf("phone-home failed after %d attempts: %w", opts.MaxAttempts, lastErr)
}

// PhoneHomeLoop is the boot-time convenience used by every plugin's
// main goroutine: register once with the default exponential-backoff
// schedule, log success, log + exit-fatal on terminal failure.
//
// It deliberately uses os.Exit because a plugin that ratd never learns
// about is useless — better to crash and let the orchestrator restart
// the container than serve a silent zombie.
//
// The default schedule (DefaultPhoneHomeOptions) is 10 attempts spread
// across ~3 minutes with exponential backoff capped at 30s. Callers who
// need a tighter or looser schedule should use PhoneHomeLoopWithOptions.
func PhoneHomeLoop(ratdInternalURL, name, addr string) {
	PhoneHomeLoopWithOptions(ratdInternalURL, name, addr, DefaultPhoneHomeOptions())
}

// PhoneHomeLoopWithOptions is PhoneHomeLoop with a custom retry schedule.
// Same exit-on-failure semantics; just lets callers override the default
// backoff curve (useful for fast-failing CI smoke tests, or for special
// plugins that need to give up sooner / wait longer).
func PhoneHomeLoopWithOptions(ratdInternalURL, name, addr string, opts PhoneHomeOptions) {
	if err := PhoneHomeWithOptions(context.Background(), ratdInternalURL, name, addr, opts); err != nil {
		slog.Error("phone-home failed after retries", "error", err)
		os.Exit(1)
	}
	slog.Info("registered with ratd", "name", name, "addr", addr)
}
