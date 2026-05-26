package main

// store holds the agent catalog in memory and persists writes back to ratd
// as plugin config. We use plugin-config as our datastore so the agent
// catalog survives container restarts without needing a mounted volume or
// schema migration. The whole catalog is one config field: `agents`.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// Agent is one configurable persona the chat plugin (and any future
// consumer) can adopt. allowed_tools is a whitelist of namespaced tool
// names ("docs__list_tables", "sql__run_query"); a single "*" entry
// means "all discovered tools". model + temperature are optional
// overrides applied per-agent on top of ai-provider's defaults.
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

type store struct {
	mu     sync.RWMutex
	byID   map[string]*Agent
	order  []string // creation order — stable list responses
	cfg    *configStore
}

func newStore(cfg *configStore) *store {
	return &store{
		byID:  map[string]*Agent{},
		order: []string{},
		cfg:   cfg,
	}
}

// hydrate replaces the in-memory state with the agents found in the
// plugin config. Called on startup AND on every config-poll tick so a
// portal-side edit to the config takes effect live.
func (s *store) hydrate(agents []Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[string]*Agent, len(agents))
	s.order = make([]string, 0, len(agents))
	for i := range agents {
		a := agents[i]
		s.byID[a.ID] = &a
		s.order = append(s.order, a.ID)
	}
}

// list returns a stable-order snapshot of the catalog.
func (s *store) list() []Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Agent, 0, len(s.order))
	for _, id := range s.order {
		if a, ok := s.byID[id]; ok {
			out = append(out, *a)
		}
	}
	return out
}

func (s *store) get(id string) (*Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

// create assigns an id, persists the new catalog, and only commits to
// memory on success. This is important on startup: if persist fails
// (e.g., ratd hasn't seen the phone-home yet), we don't leave the
// in-memory store with phantom agents that get wiped on the next
// config-poll hydrate.
func (s *store) create(ctx context.Context, in Agent) (*Agent, error) {
	if err := validate(&in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if in.ID == "" {
		in.ID = "agent_" + randomHex(6)
	}
	if _, exists := s.byID[in.ID]; exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("agent id %q already exists", in.ID)
	}
	// Build the would-be snapshot without mutating in-memory yet.
	snapshot := append(s.snapshotLocked(), in)
	s.mu.Unlock()

	if err := s.cfg.persist(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}

	// Persist OK — commit.
	s.mu.Lock()
	s.byID[in.ID] = &in
	s.order = append(s.order, in.ID)
	s.mu.Unlock()
	return &in, nil
}

// update mutates an existing agent, preserving id and creation order.
// Same persist-first-then-commit pattern as create().
func (s *store) update(ctx context.Context, id string, in Agent) (*Agent, error) {
	in.ID = id
	if err := validate(&in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if _, ok := s.byID[id]; !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("agent %q not found", id)
	}
	snapshot := s.snapshotLocked()
	for i := range snapshot {
		if snapshot[i].ID == id {
			snapshot[i] = in
			break
		}
	}
	s.mu.Unlock()

	if err := s.cfg.persist(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}

	s.mu.Lock()
	s.byID[id] = &in
	s.mu.Unlock()
	return &in, nil
}

func (s *store) delete(ctx context.Context, id string) error {
	s.mu.Lock()
	if _, ok := s.byID[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("agent %q not found", id)
	}
	snapshot := s.snapshotLocked()
	filtered := make([]Agent, 0, len(snapshot))
	for _, a := range snapshot {
		if a.ID != id {
			filtered = append(filtered, a)
		}
	}
	s.mu.Unlock()

	if err := s.cfg.persist(ctx, filtered); err != nil {
		return fmt.Errorf("persist: %w", err)
	}

	s.mu.Lock()
	delete(s.byID, id)
	out := s.order[:0]
	for _, oid := range s.order {
		if oid != id {
			out = append(out, oid)
		}
	}
	s.order = out
	s.mu.Unlock()
	return nil
}

// snapshotLocked builds a list copy. Caller must hold the lock.
func (s *store) snapshotLocked() []Agent {
	out := make([]Agent, 0, len(s.order))
	for _, id := range s.order {
		if a, ok := s.byID[id]; ok {
			out = append(out, *a)
		}
	}
	return out
}

// validate enforces the minimum invariants every Agent must satisfy.
func validate(a *Agent) error {
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	if a.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required")
	}
	if a.AllowedTools == nil {
		a.AllowedTools = []string{"*"}
	}
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// seedAgents are the defaults the plugin inserts on first run if the
// config is empty. The chat header picker shows these so a fresh
// install has something to switch between out of the box.
func seedAgents() []Agent {
	return []Agent{
		{
			ID:   "generalist",
			Name: "Generalist", Icon: "sparkles",
			Description:  "Everything-tools default — useful for open-ended questions.",
			SystemPrompt: "You are a data analyst assistant for RAT, a data platform. Use any tool you need to answer the question. Be concise. When you query, briefly explain what you queried and what the result means.",
			AllowedTools: []string{"*"},
		},
		{
			ID:   "data_explorer",
			Name: "Data Explorer", Icon: "compass",
			Description:  "Read-only catalog browsing and light sampling. Best when you don't know what's in there yet.",
			SystemPrompt: "You help the user understand what data exists in RAT. Prefer describe_warehouse / list_tables / get_table_description / get_table_schema. Use sample_table sparingly to peek at real rows. Do not write big aggregation queries — defer those to the Analyst agent. Keep answers short and structured.",
			AllowedTools: []string{
				"docs__list_namespaces", "docs__list_tables", "docs__describe_warehouse",
				"docs__get_table_schema", "docs__get_table_description",
				"sql__sample_table",
			},
		},
		{
			ID:   "analyst",
			Name: "Analyst", Icon: "calculator",
			Description:  "Read-only SQL + docs. The workhorse for 'compute me an answer' questions.",
			SystemPrompt: "You are a careful data analyst. Before writing a query, check the table's schema (get_table_schema) and a sample (sample_table) if you're unsure of the data. Then write one well-formed SQL query (use sql__run_query) and explain what it computes. Always reference tables as namespace.layer.name.",
			AllowedTools: []string{
				"docs__list_tables", "docs__get_table_schema", "docs__get_table_description",
				"sql__run_query", "sql__sample_table", "sql__explain_query",
			},
		},
	}
}
