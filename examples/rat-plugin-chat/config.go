package main

// configStore mirrors the pattern from rat-plugin-ai-provider — the plugin
// polls ratd for its own config (the portal stores it but doesn't push it),
// and merges any non-empty fields over the defaults. Just one field for now:
// an override for the system prompt.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type chatConfig struct {
	SystemPrompt string `json:"system_prompt"`
}

// configSchemaJSON is what Describe advertises so the portal renders a
// settings form for this plugin.
const configSchemaJSON = `{
  "type": "object",
  "title": "Chat settings",
  "properties": {
    "system_prompt": {
      "type": "string",
      "format": "markdown",
      "title": "System prompt",
      "description": "Optional override for the system prompt sent to the model. Leave empty to use the built-in default, which is tuned for tool-use against the RAT MCP servers."
    }
  }
}`

type configStore struct {
	mu       sync.RWMutex
	cfg      chatConfig
	defaults chatConfig
	ratdURL  string
	name     string
	http     *http.Client
}

func newConfigStore(ratdURL, name string, defaults chatConfig) *configStore {
	return &configStore{
		cfg:      defaults,
		defaults: defaults,
		ratdURL:  strings.TrimRight(ratdURL, "/"),
		name:     name,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *configStore) get() chatConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *configStore) refresh(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.ratdURL+"/api/v1/plugins/"+s.name, nil)
	if err != nil {
		return
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return
	}
	var entry struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return
	}
	var stored chatConfig
	if len(entry.Config) > 0 {
		_ = json.Unmarshal(entry.Config, &stored)
	}
	next := s.defaults
	if v := strings.TrimSpace(stored.SystemPrompt); v != "" {
		next.SystemPrompt = v
	}

	s.mu.Lock()
	changed := next != s.cfg
	s.cfg = next
	s.mu.Unlock()
	if changed {
		slog.Info("chat config updated", "system_prompt_len", len(next.SystemPrompt))
	}
}

func (s *configStore) poll(ctx context.Context, interval time.Duration) {
	for {
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		s.refresh(rctx)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
