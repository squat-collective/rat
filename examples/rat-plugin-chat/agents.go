package main

// agentsClient is a thin read-side wrapper around the rat-plugin-agents
// HTTP API. The chat plugin uses it to fetch the catalog (for the
// header picker) and to look up the agent selected for a chat turn so
// the orchestrator can apply its system_prompt + tool whitelist.
//
// The agents plugin also registers an "agents.list" interconnect
// capability, but going direct is simpler and lower-latency for a
// hot-path call — and the chat plugin is the only consumer for now.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Agent mirrors the shape rat-plugin-agents serves. We keep a parallel
// struct rather than importing — both plugins compile standalone.
type Agent struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Icon         string   `json:"icon"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	AllowedTools []string `json:"allowed_tools"`
	Model        string   `json:"model,omitempty"`
	Temperature  float64  `json:"temperature,omitempty"`
}

// allowsAll returns true if this agent has the catch-all "*" tool entry
// (and thus should see every discovered MCP tool).
func (a *Agent) allowsAll() bool {
	for _, t := range a.AllowedTools {
		if t == "*" {
			return true
		}
	}
	return false
}

// allows checks if a specific namespaced tool name passes the whitelist.
func (a *Agent) allows(nsName string) bool {
	if a.allowsAll() {
		return true
	}
	for _, t := range a.AllowedTools {
		if t == nsName {
			return true
		}
	}
	return false
}

type agentsClient struct {
	ratdURL string
	http    *http.Client

	mu     sync.RWMutex
	cache  []Agent
	byID   map[string]Agent
	loaded time.Time
}

func newAgentsClient(ratdURL string) *agentsClient {
	return &agentsClient{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
		byID:    map[string]Agent{},
	}
}

// list returns the catalog, refreshing if the cache is older than 10s.
// 10s is a reasonable trade-off — fresh enough that adding an agent in
// /x/agents shows up quickly, but rare enough that a busy chat session
// doesn't hammer the agents plugin.
func (c *agentsClient) list(ctx context.Context) ([]Agent, error) {
	c.mu.RLock()
	fresh := time.Since(c.loaded) < 10*time.Second && c.cache != nil
	c.mu.RUnlock()
	if fresh {
		c.mu.RLock()
		defer c.mu.RUnlock()
		out := make([]Agent, len(c.cache))
		copy(out, c.cache)
		return out, nil
	}
	return c.refresh(ctx)
}

// get returns one agent by id. Uses the cache, refreshing if absent.
func (c *agentsClient) get(ctx context.Context, id string) (*Agent, error) {
	if id == "" {
		return nil, nil
	}
	c.mu.RLock()
	if a, ok := c.byID[id]; ok {
		c.mu.RUnlock()
		return &a, nil
	}
	c.mu.RUnlock()
	if _, err := c.refresh(ctx); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	a, ok := c.byID[id]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	return &a, nil
}

func (c *agentsClient) refresh(ctx context.Context) ([]Agent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.ratdURL+"/api/v1/x/agents/agents", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agents plugin unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode >= 300 {
		// Soft-fail: if agents plugin isn't installed, return an empty
		// catalog rather than erroring — chat falls back to "no agent
		// selected" which means "use defaults".
		if resp.StatusCode == 404 {
			c.commit(nil)
			return nil, nil
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var out struct {
		Agents []Agent `json:"agents"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode agents: %w", err)
	}
	c.commit(out.Agents)
	return out.Agents, nil
}

func (c *agentsClient) commit(agents []Agent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = agents
	c.byID = make(map[string]Agent, len(agents))
	for _, a := range agents {
		c.byID[a.ID] = a
	}
	c.loaded = time.Now()
}
