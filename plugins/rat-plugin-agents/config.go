package main

// configStore is this plugin's bridge to ratd's plugin-config mechanism.
// Reading: GET /api/v1/plugins/{self} returns the stored config; the
// `agents` field is our catalog. Writing: PUT /api/v1/plugins/{self}/config
// replaces it — the store calls persist() after every CRUD operation.
//
// Using plugin-config as the datastore means the catalog survives
// container restarts without needing a mounted volume or a separate
// database schema. The portal's plugin-config editor will show a raw
// JSON view; users typically edit agents at /x/agents instead.

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
	mu      sync.RWMutex
	agents  []Agent
	ratdURL string
	name    string
	http    *http.Client

	// hydrateCb is called on every successful poll with the current agents
	// list — the in-memory store rebuilds itself from this.
	hydrateCb func([]Agent)
}

func newConfigStore(ratdURL, name string) *configStore {
	return &configStore{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		name:    name,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// onChange registers the callback the config poll invokes whenever the
// agents list changes server-side (or on the initial hydration).
func (c *configStore) onChange(cb func([]Agent)) { c.hydrateCb = cb }

// refresh pulls the plugin's stored config from ratd, decodes it, and
// fires hydrateCb if the agents list changed.
func (c *configStore) refresh(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.ratdURL+"/api/v1/plugins/"+c.name, nil)
	if err != nil {
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return // ratd unreachable — keep current
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
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
		Agents []Agent `json:"agents"`
	}
	if len(entry.Config) > 0 {
		_ = json.Unmarshal(entry.Config, &stored)
	}

	c.mu.Lock()
	changed := agentsDiffer(c.agents, stored.Agents)
	c.agents = stored.Agents
	cb := c.hydrateCb
	c.mu.Unlock()
	if changed && cb != nil {
		slog.Info("agents catalog refreshed from ratd", "count", len(stored.Agents))
		cb(stored.Agents)
	}
}

// persist writes the new agents list back to ratd via PUT
// /api/v1/plugins/{self}/config. The store calls this after every
// successful CRUD operation.
func (c *configStore) persist(ctx context.Context, agents []Agent) error {
	c.mu.Lock()
	c.agents = agents
	c.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"agents": agents})
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

// poll runs refresh on a tick until ctx is done.
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

// agentsDiffer compares two slices by ID + a few fields — good enough to
// avoid spamming the hydrate callback on noise.
func agentsDiffer(a, b []Agent) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].ID != b[i].ID ||
			a[i].Name != b[i].Name ||
			a[i].SystemPrompt != b[i].SystemPrompt ||
			!stringSlicesEqual(a[i].AllowedTools, b[i].AllowedTools) {
			return true
		}
	}
	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// configSchemaJSON tells the portal's plugin-config editor what the
// stored config looks like. We deliberately keep it as a raw JSON blob
// rather than a form — the editing surface lives at /x/agents.
const configSchemaJSON = `{
  "type": "object",
  "title": "Agent catalog",
  "properties": {
    "agents": {
      "type": "array",
      "title": "Agents",
      "description": "Agent catalog. Edit at /x/agents — the form there is much friendlier than this raw JSON."
    }
  }
}`
