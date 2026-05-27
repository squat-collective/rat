package main

// configStore mirrors the pattern from rat-plugin-secrets / pg-sync:
// the event ring buffer is the plugin's own ratd config. Survives
// restarts thanks to the COALESCE patch in ratd's UpsertPlugin.

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

	mu        sync.Mutex
	hydrateCb func([]Event)
}

func newConfigStore(ratdURL, name string) *configStore {
	return &configStore{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		name:    name,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *configStore) onChange(cb func([]Event)) {
	c.mu.Lock()
	c.hydrateCb = cb
	c.mu.Unlock()
}

func (c *configStore) refresh(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.ratdURL+"/api/v1/plugins/"+c.name, nil)
	if err != nil {
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return
	}
	var entry struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return
	}
	var stored struct {
		Events []Event `json:"events"`
	}
	if len(entry.Config) > 0 {
		_ = json.Unmarshal(entry.Config, &stored)
	}
	c.mu.Lock()
	cb := c.hydrateCb
	c.mu.Unlock()
	if cb != nil {
		cb(stored.Events)
	}
}

func (c *configStore) persistEvents(ctx context.Context, evs []Event) error {
	body, _ := json.Marshal(map[string]any{"events": evs})
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

var _ = slog.Default
