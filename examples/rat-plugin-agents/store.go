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
//
// subagents is a list of agent IDs this agent may delegate to. When
// non-empty, the chat orchestrator exposes each one as an "agent__<id>"
// tool — the parent can call it like any other tool and the
// orchestrator recursively runs that subagent (with a depth cap) and
// returns its final answer.
//
// disabled (not enabled) is used so the JSON-default zero value means
// "enabled" — existing agents stored before this field existed don't
// suddenly disappear.
type Agent struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Icon             string   `json:"icon"`
	Description      string   `json:"description"`
	SystemPrompt     string   `json:"system_prompt"`
	AllowedTools     []string `json:"allowed_tools"`
	Subagents        []string `json:"subagents,omitempty"`
	MaxIterations    int      `json:"max_iterations,omitempty"`
	Disabled         bool     `json:"disabled,omitempty"`
	Color            string   `json:"color,omitempty"`
	ExampleQuestions []string `json:"example_questions,omitempty"`
	Model            string   `json:"model,omitempty"`
	Temperature      float64  `json:"temperature,omitempty"`
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
	if err := s.validateAgainstCatalog(&in); err != nil {
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
	if err := s.validateAgainstCatalog(&in); err != nil {
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
// subagent IDs are validated against the live catalog (must exist; no
// self-reference). One-step cycle detection is here too — deeper cycles
// are caught at runtime by the orchestrator's depth cap.
func (s *store) validateAgainstCatalog(a *Agent) error {
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	if a.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required")
	}
	if a.AllowedTools == nil {
		a.AllowedTools = []string{"*"}
	}
	if a.MaxIterations < 0 {
		return fmt.Errorf("max_iterations cannot be negative")
	}
	if a.MaxIterations > 32 {
		return fmt.Errorf("max_iterations cannot exceed 32 (safety cap)")
	}
	// Subagent references: each id must point at an existing agent and
	// must not be self.
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range a.Subagents {
		if sub == a.ID {
			return fmt.Errorf("agent cannot reference itself as a subagent")
		}
		if _, ok := s.byID[sub]; !ok {
			return fmt.Errorf("subagent %q does not exist", sub)
		}
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
// install has something to switch between out of the box. Includes a
// Coordinator that demonstrates subagent delegation.
func seedAgents() []Agent {
	return []Agent{
		{
			ID:   "generalist",
			Name: "Generalist", Icon: "sparkles", Color: "#22c55e",
			Description:  "Everything-tools default — useful for open-ended questions.",
			SystemPrompt: generalistPrompt,
			AllowedTools: []string{"*"},
			ExampleQuestions: []string{
				"What tables do we have?",
				"How many rows are in each shop table?",
				"Show me the schema of shop.silver.orders_enriched",
			},
		},
		{
			ID:   "data_explorer",
			Name: "Data Explorer", Icon: "compass", Color: "#3b82f6",
			Description:  "Read-only catalog browsing and light sampling. Best when you don't know what's in there yet.",
			SystemPrompt: dataExplorerPrompt,
			AllowedTools: []string{
				"docs__list_namespaces", "docs__list_tables", "docs__describe_warehouse",
				"docs__get_table_schema", "docs__get_table_description",
				"sql__sample_table",
			},
			ExampleQuestions: []string{
				"Give me an overview of the warehouse",
				"What's in the cosmos namespace?",
				"Show me a sample of shop.bronze.orders",
			},
		},
		{
			ID:   "analyst",
			Name: "Analyst", Icon: "calculator", Color: "#a855f7",
			Description:  "Read-only SQL + docs. The workhorse for 'compute me an answer' questions.",
			SystemPrompt: analystPrompt,
			AllowedTools: []string{
				"docs__list_tables", "docs__get_table_schema", "docs__get_table_description",
				"sql__run_query", "sql__sample_table", "sql__explain_query",
			},
			ExampleQuestions: []string{
				"Total revenue per month in 2024 from shop",
				"Top 5 customers in shop by lifetime value",
				"Compare order counts by status in shop",
			},
		},
		{
			ID:   "coordinator",
			Name: "Coordinator", Icon: "git-fork", Color: "#f59e0b",
			Description:  "Routes the question to a specialist subagent (Explorer or Analyst) instead of doing the work itself. Good for ambiguous or multi-step prompts.",
			SystemPrompt: coordinatorPrompt,
			AllowedTools: []string{}, // no MCP tools — only subagent tools
			Subagents:    []string{"data_explorer", "analyst"},
			ExampleQuestions: []string{
				"What's in the underground namespace and how many tracks have a high rating?",
				"Tell me everything about shop.silver.orders_enriched and give me last month's revenue",
			},
		},
	}
}

// System prompts are split out as constants because the seed catalog and
// the live PATCH-update flow both reference the same text — and the
// anti-hallucination wording is something we'll iterate on.

const generalistPrompt = `You are a data assistant for RAT, a data platform.

ABSOLUTE RULE — NEVER INVENT: every concrete fact about tables, columns, row counts, or data values MUST come from a tool call in this turn. You have NO memorised knowledge of any specific table in this warehouse. If you describe a table, column, or value without first calling a tool that returned it, you have hallucinated.

When the user asks about specific data, use the docs__ tools to learn the catalog and the sql__ tools to compute results. If a tool call fails, report the error verbatim — never guess what the answer "would have been".

Be concise. When you query, briefly explain what you queried and what the result means.`

const dataExplorerPrompt = `You are the Data Explorer agent for RAT. Your job is to surface real warehouse facts to the user.

ABSOLUTE RULE — NEVER INVENT: you have NO memorised knowledge of any table in this warehouse. NEVER describe a table, column, or sample row from prior knowledge — every factual claim MUST come from a tool call in this turn.

For a "describe / what's in / show me" question, your required steps:
  1. Call docs__get_table_schema(namespace, layer, name) to get the REAL columns and types.
  2. Call docs__get_table_description(namespace, layer, name) for any human-authored notes.
  3. If the user wants a sample, call sql__sample_table(namespace, layer, name, limit=10).
  4. Present the results, citing what the tools returned.

If you do not know which namespace.layer.name to use, call docs__list_tables or docs__describe_warehouse FIRST.

If any tool call fails, report the error verbatim. Never "fill in" what you think the answer would be. Defer big aggregation queries to the Analyst agent.`

const analystPrompt = `You are the Analyst agent for RAT. Your job is to compute real answers from real data using SQL.

ABSOLUTE RULE — NEVER INVENT: you have NO memorised knowledge of any table. NEVER write SQL using column names you have not seen in a tool response this turn. NEVER report a query result you did not actually run.

For an "answer this / compute / how many / what is the total" task:
  1. If you do not already know the exact columns of the table you need, call docs__get_table_schema(namespace, layer, name) FIRST.
  2. Write SQL using only columns you saw in that response.
  3. Call sql__run_query(sql) to actually execute the query.
  4. Report the actual numeric result from the tool output.

If sql__run_query returns an error, READ the error message and fix the SQL (often a wrong column name) — then call again. Do NOT explain what the query "would do" without running it. Returning an explanation instead of a result is a FAILURE of the task.

Reference tables as namespace.layer.name (e.g. shop.silver.orders_enriched). Always include a sensible LIMIT.`

const coordinatorPrompt = `You are a routing agent. You DO NOT call data tools yourself — you delegate to subagents via the agent__<id> tools and synthesise their answers.

Available subagents:
  - agent__data_explorer for "what data exists / describe X / show me Y" questions.
  - agent__analyst for "compute / count / sum / aggregate" questions and anything requiring SQL.

When delegating:
  - Hand each subagent a FOCUSED, SELF-CONTAINED task. They cannot see this conversation.
  - For multi-step questions, decompose into 2-3 calls (e.g. "describe table X", then "compute the monthly total from X").
  - ALWAYS pass the exact namespace.layer.name. Never make up a path.

CRITICAL — VERIFY subagent outputs before trusting them:
  - If a subagent's answer describes a table with generic columns you have NOT seen come from a tool (e.g. "total_amount", "user_id", "shipping_address" without justification) → it hallucinated. Re-task it with: "You MUST call docs__get_table_schema before describing this table. Do not invent columns. The real columns will be in the tool response."
  - If a subagent returns empty content or just explains "what the query would do" without a number → it failed to execute. Re-task it with: "Call sql__run_query and report the actual numeric result the tool returned."
  - If the answer looks specific and grounded in real tool output → synthesise it for the user.

Be concise in your final synthesis. Cite the actual numbers and column names the subagents returned.`
