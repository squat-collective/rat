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

// defaultMaxEvents is the event-buffer size before a max is configured.
const defaultMaxEvents = 50

// notifierConfig is the plugin's portal-configurable settings.
type notifierConfig struct {
	WebhookURL          string `json:"webhook_url"`
	MaxEvents           int    `json:"max_events"`
	ForwardOnlyFailures bool   `json:"forward_only_failures"`
}

// configSchemaJSON is the JSON Schema advertised in Describe — the portal's
// plugin-config editor renders the settings form straight from it.
const configSchemaJSON = `{
  "type": "object",
  "title": "Event notifier settings",
  "properties": {
    "webhook_url": {
      "type": "string",
      "title": "Webhook URL",
      "description": "Events are POSTed here as JSON. Leave empty to only record them in the portal."
    },
    "max_events": {
      "type": "integer",
      "title": "Events to keep",
      "description": "How many recent events to retain for the Events page (1-1000)."
    },
    "forward_only_failures": {
      "type": "boolean",
      "title": "Forward only failures",
      "description": "When on, only failure events (quality_failed, failed runs) are sent to the webhook."
    }
  }
}`

// configStore holds the plugin's current effective config and refreshes it
// from ratd. RAT stores plugin config but does not push it to plugins — so a
// configurable plugin polls GET /api/v1/plugins/{self} for its own config.
type configStore struct {
	mu       sync.RWMutex
	cfg      notifierConfig
	defaults notifierConfig

	ratdURL string
	name    string
	http    *http.Client
}

func newConfigStore(ratdURL, name string, defaults notifierConfig) *configStore {
	return &configStore{
		cfg:      defaults,
		defaults: defaults,
		ratdURL:  strings.TrimRight(ratdURL, "/"),
		name:     name,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *configStore) get() notifierConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// refresh pulls the plugin's config from ratd and merges it over the defaults.
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
	var stored notifierConfig
	if len(entry.Config) > 0 {
		_ = json.Unmarshal(entry.Config, &stored)
	}
	next := merge(s.defaults, stored)

	s.mu.Lock()
	changed := next != s.cfg
	s.cfg = next
	s.mu.Unlock()
	if changed {
		slog.Info("config updated from ratd",
			"webhook_configured", next.WebhookURL != "",
			"max_events", next.MaxEvents,
			"forward_only_failures", next.ForwardOnlyFailures)
	}
}

// merge overlays stored config on the defaults. An empty/zero stored field
// falls back to its default, so the plugin always has a working config.
func merge(def, stored notifierConfig) notifierConfig {
	out := def
	if v := strings.TrimSpace(stored.WebhookURL); v != "" {
		out.WebhookURL = v
	}
	if stored.MaxEvents > 0 {
		out.MaxEvents = stored.MaxEvents
		if out.MaxEvents > 1000 {
			out.MaxEvents = 1000
		}
	}
	// A boolean cannot be "unset" once the config object exists; its zero value
	// (false) is also the default, so the stored value always wins.
	out.ForwardOnlyFailures = stored.ForwardOnlyFailures
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
