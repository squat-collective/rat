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
	disco   *discoverer
	orch    *orchestrator
	cfg     *configStore
	agents  *agentsClient
	convs   *conversationStore
	subRuns *subagentRunStore
}

func newAPI(disco *discoverer, orch *orchestrator, cfg *configStore, agents *agentsClient, convs *conversationStore, subRuns *subagentRunStore) *api {
	return &api{disco: disco, orch: orch, cfg: cfg, agents: agents, convs: convs, subRuns: subRuns}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /servers", a.listServers)
	m.HandleFunc("GET /tools", a.listTools)
	m.HandleFunc("GET /agents", a.listAgents)
	m.HandleFunc("GET /conversations", a.listConversations)
	m.HandleFunc("GET /conversations/{id}", a.getConversation)
	m.HandleFunc("POST /conversations", a.createConversation)
	m.HandleFunc("PATCH /conversations/{id}", a.renameConversation)
	m.HandleFunc("DELETE /conversations/{id}", a.deleteConversation)
	m.HandleFunc("GET /conversations/{id}/subagent-runs", a.listSubagentRuns)
	m.HandleFunc("GET /subagent-runs/{id}", a.getSubagentRun)
	m.HandleFunc("POST /chat", a.chat)
	m.HandleFunc("GET /config", a.getConfig)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// ── Conversation CRUD ─────────────────────────────────────────────

func (a *api) listConversations(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"conversations": a.convs.list()})
}

func (a *api) getConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, ok := a.convs.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *api) createConversation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentID string `json:"agent_id"`
		Title   string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	c, err := a.convs.create(in.AgentID, in.Title)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *api) renameConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	c, err := a.convs.rename(id, in.Title)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *api) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.convs.delete(id); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listSubagentRuns returns summaries of every subagent invocation that
// happened during a given conversation (oldest first). The Events field
// is stripped; clients fetch full traces individually with GET
// /subagent-runs/{id}.
func (a *api) listSubagentRuns(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	writeJSON(w, http.StatusOK, map[string]any{
		"runs": a.subRuns.listForConversation(convID),
	})
}

// getSubagentRun returns one full trace, including every event the
// orchestrator emitted during the subagent's chat loop (tool_calls,
// tool_results, assistant_message deltas, etc).
func (a *api) getSubagentRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.subRuns.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "subagent run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
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
		Messages       []chatMessage `json:"messages"`
		SystemPrompt   string        `json:"system_prompt"`
		AgentID        string        `json:"agent_id"`
		ConversationID string        `json:"conversation_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4*1024*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(in.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages is required")
		return
	}

	// Resolve (or create) the conversation. The conversation is the
	// authoritative message history: the client only ever sends *one*
	// new message at the end of in.Messages, and we splice it onto the
	// persisted history.
	var conv *Conversation
	if in.ConversationID != "" {
		if c, ok := a.convs.get(in.ConversationID); ok {
			conv = c
		}
	}
	if conv == nil {
		var err error
		// Derive the initial title from the first user message in the request.
		title := ""
		for _, m := range in.Messages {
			if m.Role == "user" && m.Content != "" {
				title = deriveTitle(m.Content)
				break
			}
		}
		conv, err = a.convs.create(in.AgentID, title)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "create conversation: "+err.Error())
			return
		}
	}
	// Append the new user message(s) — the orchestrator will append
	// assistant + tool turns as the loop runs, then we persist at the end.
	newMessages := in.Messages
	if len(conv.Messages) > 0 && len(newMessages) > 0 {
		// Client may have sent the full prior transcript + the new msg;
		// trust the persisted history and take only any messages it doesn't
		// already have. Simplest: take the last user message.
		for i := len(newMessages) - 1; i >= 0; i-- {
			if newMessages[i].Role == "user" {
				newMessages = []chatMessage{newMessages[i]}
				break
			}
		}
	}
	conv.Messages = append(conv.Messages, newMessages...)
	if conv.AgentID == "" && in.AgentID != "" {
		conv.AgentID = in.AgentID
	}
	if conv.Title == "" && len(conv.Messages) > 0 {
		for _, m := range conv.Messages {
			if m.Role == "user" && m.Content != "" {
				conv.Title = deriveTitle(m.Content)
				break
			}
		}
	}
	if err := a.convs.save(conv); err != nil {
		// Non-fatal — the chat can still run even if persist fails.
		_ = err
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

	// Emit a conversation event up front so the UI can capture the id
	// even if it was server-assigned. The orchestrator's own "started"
	// event follows.
	_ = sink.emit("conversation", map[string]any{
		"id": conv.ID, "title": conv.Title, "agent_id": conv.AgentID,
	})

	// Wrap the sink so we can capture assistant + tool messages as they
	// arrive and persist them — gives us a partial transcript even if
	// the client disconnects mid-stream.
	tracker := &convTracker{inner: sink, conv: conv, store: a.convs}
	_ = a.orch.chatTurn(ctx, tracker, conv.Messages, sysPrompt, in.AgentID, 0, conv.ID)

	// Final save after the loop finishes — captures the last assistant
	// message and any post-loop bookkeeping.
	_ = a.convs.save(conv)
}

// convTracker watches the SSE stream from the orchestrator and mirrors
// each assistant/tool message into the conversation, persisting on
// every terminal event. The original sink still gets every event
// untouched.
type convTracker struct {
	inner sseSink
	conv  *Conversation
	store *conversationStore
}

func (t *convTracker) emit(event string, payload any) error {
	switch event {
	case "assistant_message":
		// payload is a chatMessage — marshal/unmarshal to coerce.
		if raw, err := json.Marshal(payload); err == nil {
			var m chatMessage
			if json.Unmarshal(raw, &m) == nil {
				t.conv.Messages = append(t.conv.Messages, m)
				_ = t.store.save(t.conv)
			}
		}
	case "tool_result":
		// payload has {tool_call_id, name, is_error, output}.
		raw, err := json.Marshal(payload)
		if err == nil {
			var tr struct {
				ToolCallID string `json:"tool_call_id"`
				Name       string `json:"name"`
				Output     string `json:"output"`
			}
			if json.Unmarshal(raw, &tr) == nil {
				t.conv.Messages = append(t.conv.Messages, chatMessage{
					Role: "tool", ToolCallID: tr.ToolCallID, Name: tr.Name, Content: tr.Output,
				})
				_ = t.store.save(t.conv)
			}
		}
	}
	return t.inner.emit(event, payload)
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
