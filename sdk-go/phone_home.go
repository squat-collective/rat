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

// PhoneHomeRetryDelay is the pause between phone-home attempts. Exposed
// so tests can override it; the production default matches the value
// every example plugin already used (2 seconds).
var PhoneHomeRetryDelay = 2 * time.Second

// PhoneHome registers the plugin with ratd's internal listener. It
// retries up to maxAttempts times with PhoneHomeRetryDelay between
// attempts. Returns nil on success or the last error after exhaustion.
//
// Honours ctx for cancellation between attempts. Each attempt uses its
// own 10s timeout so a stuck ratd can't pin the registration loop.
func PhoneHome(ctx context.Context, ratdInternalURL, name, addr string, maxAttempts int) error {
	endpoint := ratdInternalURL + "/internal/plugins/register"
	body, err := json.Marshal(map[string]string{"name": name, "addr": addr})
	if err != nil {
		return fmt.Errorf("marshal register body: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			if lastErr == nil {
				return ctx.Err()
			}
			return fmt.Errorf("phone-home cancelled after %d attempts: %w (last attempt: %v)", attempt-1, ctx.Err(), lastErr)
		case <-time.After(PhoneHomeRetryDelay):
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
	return fmt.Errorf("phone-home failed after %d attempts: %w", maxAttempts, lastErr)
}

// PhoneHomeLoop is the boot-time convenience used by every plugin's
// main goroutine: register once with the standard 30-attempt retry
// budget, log success, log + exit-fatal on terminal failure.
//
// It deliberately uses os.Exit because a plugin that ratd never learns
// about is useless — better to crash and let the orchestrator restart
// the container than serve a silent zombie.
func PhoneHomeLoop(ratdInternalURL, name, addr string) {
	if err := PhoneHome(context.Background(), ratdInternalURL, name, addr, 30); err != nil {
		slog.Error("phone-home failed after retries", "error", err)
		os.Exit(1)
	}
	slog.Info("registered with ratd", "name", name, "addr", addr)
}
