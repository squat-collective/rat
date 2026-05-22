package main

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

// aiConfig is the plugin's configurable settings. The portal renders a form
// for these from configSchemaJSON and saves them via
// PUT /api/v1/plugins/{name}/config; the plugin polls ratd to pick them up.
type aiConfig struct {
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
}

// configSchemaJSON is the JSON Schema advertised in Describe. The portal's
// plugin-config editor renders a settings form straight from it — this plugin
// is the first to use that mechanism.
const configSchemaJSON = `{
  "type": "object",
  "title": "AI provider settings",
  "properties": {
    "base_url": {
      "type": "string",
      "title": "API base URL",
      "description": "An OpenAI-compatible /v1 endpoint (Ollama, OpenAI, vLLM, ...)."
    },
    "api_key": {
      "type": "string",
      "title": "API key",
      "description": "Bearer token for the API. Ollama ignores it."
    },
    "model": {
      "type": "string",
      "title": "Model",
      "description": "Default model name, e.g. gpt-oss:20b."
    },
    "system_prompt": {
      "type": "string",
      "title": "Default system prompt",
      "description": "Used by /complete when the caller passes no system prompt."
    }
  }
}`

// configStore holds the plugin's current effective config and refreshes it
// from ratd. RAT stores plugin config but does not push it to plugins — so a
// configurable plugin polls GET /api/v1/plugins/{self} for its own config.
type configStore struct {
	mu       sync.RWMutex
	cfg      aiConfig
	defaults aiConfig

	ratdURL string
	name    string
	http    *http.Client
}

func newConfigStore(ratdURL, name string, defaults aiConfig) *configStore {
	return &configStore{
		cfg:      defaults,
		defaults: defaults,
		ratdURL:  strings.TrimRight(ratdURL, "/"),
		name:     name,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *configStore) get() aiConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// refresh pulls the plugin's config from ratd and merges it over the defaults.
// An empty stored field falls back to its default, so the plugin always has a
// working config — even before anything is set in the portal.
func (s *configStore) refresh(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.ratdURL+"/api/v1/plugins/"+s.name, nil)
	if err != nil {
		return
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return // ratd unreachable — keep the current config
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return // not registered yet — keep the current config
	}

	var entry struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return
	}
	var stored aiConfig
	if len(entry.Config) > 0 {
		_ = json.Unmarshal(entry.Config, &stored)
	}
	next := merge(s.defaults, stored)

	s.mu.Lock()
	changed := next != s.cfg
	s.cfg = next
	s.mu.Unlock()
	if changed {
		slog.Info("config updated from ratd", "base_url", next.BaseURL, "model", next.Model)
	}
}

// merge overlays stored config on the defaults — non-empty stored fields win.
func merge(def, stored aiConfig) aiConfig {
	out := def
	if v := strings.TrimSpace(stored.BaseURL); v != "" {
		out.BaseURL = v
	}
	if v := strings.TrimSpace(stored.APIKey); v != "" {
		out.APIKey = v
	}
	if v := strings.TrimSpace(stored.Model); v != "" {
		out.Model = v
	}
	if v := strings.TrimSpace(stored.SystemPrompt); v != "" {
		out.SystemPrompt = v
	}
	return out
}

// poll refreshes the config from ratd every interval until ctx is done.
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
