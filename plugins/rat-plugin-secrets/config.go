package main

// configStore is the plugin's bridge to ratd's plugin-config mechanism.
// The encrypted secret list IS the config — no secondary database. Loss
// of the encryption key would render the ciphertexts useless even if
// you could read ratd's database, which is the property we want.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type configStore struct {
	ratdURL string
	name    string
	http    *http.Client

	mu        sync.RWMutex
	secrets   []Secret
	hydrateCb func([]Secret)
}

func newConfigStore(ratdURL, name string) *configStore {
	return &configStore{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		name:    name,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *configStore) onChange(cb func([]Secret)) { c.hydrateCb = cb }

func (c *configStore) refresh(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.ratdURL+"/api/v1/plugins/"+c.name, nil)
	if err != nil {
		slog.Warn("config refresh: build request", "error", err)
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("config refresh: ratd unreachable", "error", err)
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if resp.StatusCode != http.StatusOK {
		slog.Warn("config refresh: non-2xx", "status", resp.StatusCode)
		return
	}
	var entry struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		slog.Warn("config refresh: outer unmarshal failed", "error", err)
		return
	}
	var stored struct {
		Secrets []Secret `json:"secrets"`
	}
	if len(entry.Config) > 0 {
		if err := json.Unmarshal(entry.Config, &stored); err != nil {
			slog.Warn("config refresh: inner unmarshal failed", "error", err,
				"raw_config", string(entry.Config))
		}
	}
	slog.Debug("config refresh complete",
		"secret_count", len(stored.Secrets), "config_bytes", len(entry.Config))

	c.mu.Lock()
	c.secrets = stored.Secrets
	cb := c.hydrateCb
	c.mu.Unlock()
	if cb != nil {
		cb(stored.Secrets)
	}
}

func (c *configStore) persist(ctx context.Context, secrets []Secret) error {
	c.mu.Lock()
	c.secrets = secrets
	c.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"secrets": secrets})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.ratdURL+"/api/v1/plugins/"+c.name+"/config", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ratd unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

func (c *configStore) poll(ctx context.Context, interval time.Duration) {
	for {
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		c.refresh(rctx)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// configSchemaJSON tells the portal's plugin-config editor what we
// store. Deliberately opaque — secrets shouldn't be edited as raw JSON
// in the portal; users go through the dedicated UI at /x/secrets.
const configSchemaJSON = `{
  "type": "object",
  "title": "Secrets (encrypted)",
  "properties": {
    "secrets": {
      "type": "array",
      "title": "Encrypted secrets",
      "description": "Edit at /x/secrets — the values shown here are encrypted ciphertexts, not the actual secrets. Do not edit by hand."
    }
  }
}`

// Discard unused import linter noise.
var _ = slog.Default
