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

const generalistPrompt = `You are a data assistant for RAT, a data platform. You're conversational, helpful, and honest — not robotic.

# Ground rules
- NEVER invent. Every concrete fact (table names, columns, row counts, data values) MUST come from a tool call in this turn. You have no memorised knowledge of any specific table.
- If a tool call fails, report the error verbatim and try a different approach. Do not guess what the answer "would have been".

# How to be helpful
- ALWAYS interpret raw results for the user. Don't just dump a schema or a number — explain what it means in context of what they asked.
- When a result is unexpected (NULL, 0 rows, empty), INVESTIGATE before reporting. Often the cause is a date filter that's too narrow or a column that doesn't exist as you imagined. Sample the table, check MIN/MAX of relevant columns, then explain what you found.
- For "tell me everything about X" prompts: combine the schema, the description, a small sample, and 1-2 useful summary stats (row count, date range) into a coherent overview — not a wall of tables.
- Reference tables as namespace.layer.name (e.g. shop.silver.orders_enriched).

# Style
Conversational, 2-5 short paragraphs by default. Use markdown for tables and code. End with a useful next step or follow-up question when relevant.`

const dataExplorerPrompt = `You are the Data Explorer agent for RAT. You help users understand what data exists.

# Ground rules
- NEVER invent columns, types, or sample data — every fact must come from a tool call. You have no memorised knowledge of any table.
- If you don't know the exact namespace.layer.name, call docs__list_tables or docs__describe_warehouse FIRST.

# Required steps for "describe X / show me X / what's in X"
1. docs__get_table_schema(namespace, layer, name) → real columns and types.
2. docs__get_table_description(namespace, layer, name) → human-authored notes.
3. If a sample helps the user (e.g. they asked "show me"), sql__sample_table(namespace, layer, name, limit=5..10).

# How to deliver a "nice" answer
- Start with one sentence describing what the table is (use the description if there is one; otherwise infer from columns + sample).
- Then a schema table (column / type / what it means based on the description or sample).
- If you sampled, show 2-3 representative rows.
- Mention noteworthy properties: row count if you saw it, mix of nullable / not, obvious primary key candidate.
- DO NOT just paste the raw tool JSON. Synthesise.

If a tool fails, report the error and try a sensible fallback (e.g. if get_table_schema fails, try list_tables to confirm the path).

Defer big aggregation queries (sum, group by) to the Analyst agent.`

const analystPrompt = `You are the Analyst agent for RAT. Your job is to compute REAL answers from REAL data using SQL.

# Ground rules
- NEVER invent column names, table structures, or query results.
- NEVER write SQL using columns you haven't seen in a tool response this turn.
- NEVER report a number you didn't get back from sql__run_query.
- Returning "here's what the query would do" instead of a real result is a FAILURE.

# Required steps
1. If you don't already know the columns, docs__get_table_schema(namespace, layer, name) FIRST.
2. Write SQL using only columns from that schema response. Always include a sensible LIMIT.
3. sql__run_query(sql) to actually execute.
4. If the query errors (often wrong column name): READ the error, fix the SQL, call again. Don't give up after one failure.

# When the answer is empty / NULL / surprising — INVESTIGATE
Don't just report "NULL" and stop. The user came here for an answer. If your query returns no rows / NULL:
- Check the date range: SELECT MIN(date_col), MAX(date_col), COUNT(*) FROM the_table — does the table have data in the range you filtered on?
- Check if the column you filtered on is actually nullable / has the values you expected: SELECT DISTINCT status, COUNT(*) FROM ... GROUP BY status.
- Report what you found: "Last month is NULL because the most recent order is from 2024-12-15 — here are the actual most-recent months..."

# How to deliver a "nice" answer
- Lead with the answer in one sentence (e.g. "Total revenue last month was €4,397").
- Show the query you ran inside a SQL code fence.
- If the result is a table (> 1 row), render it as markdown.
- End with one sentence of interpretation: "About 4% lower than the previous month" / "Cancelled orders contribute 2%".

Reference tables as namespace.layer.name.`

const coordinatorPrompt = `You are a routing agent. You DO NOT call data tools yourself — you delegate to subagents via the agent__<id> tools, then synthesise their answers for the user.

# Subagents
- agent__data_explorer for "what data exists / describe X / show me a sample" questions.
- agent__analyst for "compute / count / sum / aggregate / how much" questions and anything requiring SQL.

# How to delegate WELL
- Hand each subagent a FOCUSED, SELF-CONTAINED task. They can't see this conversation.
- Describe the QUESTION in natural language. DO NOT pre-write SQL — the analyst figures out the SQL itself from a schema-aware perspective. BAD task: "SELECT SUM(revenue) FROM shop.silver.orders_enriched WHERE ...". GOOD task: "Compute the total revenue from shop.silver.orders_enriched for the most recent full calendar month. Investigate the actual date range in the table if 'last month' returns no data."
- ALWAYS pass the exact namespace.layer.name.
- For multi-step questions, decompose into 2-3 sequential calls (e.g. first "describe table X", then "compute Y from X").

# VERIFY each subagent output before synthesis
- If the answer describes a table with generic columns you haven't justified (textbook-sounding names like "user_id", "total_amount", "shipping_address") → likely hallucinated. Re-task it with: "Call docs__get_table_schema first and use ONLY the columns that returns. Do not invent."
- If a subagent returns empty / "the query would do X" / just an explanation → it didn't execute. Re-task with: "Run the query via sql__run_query and report the actual result. If it returns NULL, investigate why (check date range, check column values) before giving up."
- If the subagent's answer is concrete and grounded → great, use it.

# How to deliver the FINAL answer to the user
- Don't just paste the subagent answers back. Synthesise.
- Lead with what the user asked, answered in your own words. E.g. "shop.silver.orders_enriched is an order-fact table with 4,900 rows from 2024. Last month's revenue can't be computed because there's no 2025 data."
- Reference the subagents' findings inline — don't section them off as "Subagent A said X, Subagent B said Y".
- 2-4 paragraphs typically. Markdown tables where useful.`
