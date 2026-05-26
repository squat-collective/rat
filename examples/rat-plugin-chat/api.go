package main

// api serves the chat plugin's REST + SSE endpoints. Mounted by ratd at
// /api/v1/x/chat/*.
//
//   GET  /servers  — discovered MCP servers + their tool catalogs
//   GET  /tools    — the flattened (namespaced) tool list the LLM sees
//   POST /chat     — runs one turn of the chat (SSE response, see chat())
//   GET  /config   — the plugin's effective config (system prompt etc.)
//   GET  /health   — liveness probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type api struct {
	disco  *discoverer
	orch   *orchestrator
	cfg    *configStore
	agents *agentsClient
}

func newAPI(disco *discoverer, orch *orchestrator, cfg *configStore, agents *agentsClient) *api {
	return &api{disco: disco, orch: orch, cfg: cfg, agents: agents}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /servers", a.listServers)
	m.HandleFunc("GET /tools", a.listTools)
	m.HandleFunc("GET /agents", a.listAgents)
	m.HandleFunc("POST /chat", a.chat)
	m.HandleFunc("GET /config", a.getConfig)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// listAgents proxies the agents plugin's catalog so the chat UI can
// render the picker without needing to know where the agents plugin
// lives. Returns an empty list if the agents plugin is absent.
func (a *api) listAgents(w http.ResponseWriter, r *http.Request) {
	out, err := a.agents.list(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"agents": []any{}, "warning": err.Error()})
		return
	}
	if out == nil {
		out = []Agent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

func (a *api) listServers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"servers": a.disco.list()})
}

func (a *api) listTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": a.disco.allTools()})
}

func (a *api) getConfig(w http.ResponseWriter, _ *http.Request) {
	c := a.cfg.get()
	writeJSON(w, http.StatusOK, map[string]any{
		"system_prompt":   c.SystemPrompt,
		"max_iterations":  defaultMaxIterations,
		"discovered_servers": len(a.disco.list()),
	})
}

// chat runs one user turn through the orchestrator and streams events back
// as Server-Sent Events. The request body is { messages, system_prompt? }.
// The events the client should expect:
//
//   event: started           — { tools_available, servers }
//   event: assistant_message — full assistant message (may have tool_calls)
//   event: tool_call         — one tool the assistant decided to invoke
//   event: tool_result       — output of that tool (or error)
//   event: done              — { finish_reason }
//   event: error             — { error }
//
// The handler returns when the SSE stream closes (orchestrator finished).
func (a *api) chat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages     []chatMessage `json:"messages"`
		SystemPrompt string        `json:"system_prompt"`
		AgentID      string        `json:"agent_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4*1024*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(in.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages is required")
		return
	}
	// Agent selection (if any) takes precedence over an explicit
	// system_prompt — the orchestrator applies the agent's prompt.
	sysPrompt := in.SystemPrompt
	if sysPrompt == "" && in.AgentID == "" {
		sysPrompt = a.cfg.get().SystemPrompt
	}
	if sysPrompt == "" && in.AgentID == "" {
		sysPrompt = defaultSystemPrompt(len(a.disco.list()), len(a.disco.allTools()))
	}

	// SSE setup. We use a Flusher (chi/stdlib both support it) so events
	// reach the browser as they're produced instead of being buffered.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported by responseWriter")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx-style buffering, just in case

	sink := &sseWriter{w: w, flusher: flusher}

	// Bound the whole chat turn — the orchestrator already bounds each AI
	// call, but if a tool hangs we want the whole thing to die eventually.
	ctx, cancel := context.WithTimeout(r.Context(), 5*60*1e9) // 5 minutes
	defer cancel()
	_ = a.orch.chatTurn(ctx, sink, in.Messages, sysPrompt, in.AgentID, 0)
}

// ── SSE sink ──────────────────────────────────────────────────────

type sseWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	closed  bool
}

func (s *sseWriter) emit(event string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("sse: closed")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
		s.closed = true
		return err
	}
	s.flusher.Flush()
	return nil
}

// defaultSystemPrompt is what the plugin uses if no system prompt was set
// in the portal's plugin config (and the caller didn't override it). It
// gives the model a quick orientation about what kind of help it's offering.
func defaultSystemPrompt(servers, tools int) string {
	return fmt.Sprintf(`You are a data analyst assistant for RAT, a data platform. You have %d MCP server(s) wired in, exposing %d tools total.

Use the tools whenever the answer requires real data:
  - Use list_namespaces / list_tables / describe_warehouse to learn what data exists.
  - Use get_table_schema and get_table_description before writing queries.
  - Use sample_table to peek at real rows.
  - Use run_query to actually compute results — always limit your output.

Be concise. When you run a query, briefly explain what you queried and what the result means. Prefer one well-formed query to many small ones.`, servers, tools)
}
