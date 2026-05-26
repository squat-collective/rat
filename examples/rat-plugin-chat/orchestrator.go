package main

// orchestrator runs the chat plugin's tool-use loop. The dance is:
//
//   1. Build the tools list from every discovered MCP server.
//   2. Call ai.chat-with-tools on the ai-provider with the conversation
//      so far + that tools list.
//   3. If finish_reason == "tool_calls", execute each requested tool via
//      its MCP server, append the assistant message + a "tool" role
//      message per result to the conversation, and go back to step 2.
//   4. Otherwise stream the assistant's final text + DONE event.
//
// We stream events to the caller as Server-Sent Events so the UI can render
// tool calls and their results live — the UX of waiting for one big JSON
// blob would be terrible with the multi-turn loop.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// safety cap on iterations — even a runaway model can't burn forever.
// Agents can override via Agent.MaxIterations; this is the fallback.
const defaultMaxIterations = 8

// maxSubagentDepth bounds A→B→C delegation chains. Even within the depth
// cap, the orchestrator's per-iteration cap still applies at each level.
const maxSubagentDepth = 3

// agentToolPrefix marks tools that delegate to a subagent. The
// orchestrator handles these specially in the tool-call loop instead of
// routing them through MCP.
const agentToolPrefix = "agent__"

// sseSink is anything that can emit one SSE event with a JSON payload.
type sseSink interface {
	emit(event string, payload any) error
}

// orchestrator wires the AI provider + the MCP discoverer together for one
// turn of the chat.
type orchestrator struct {
	ratdURL       string
	mcp           *mcpClient
	disco         *discoverer
	agents        *agentsClient
	subRuns       *subagentRunStore  // nil-safe: subagent traces just don't get persisted
	continuations *continuationStore // nil-safe: chatTurn skips the prompt if absent
	http          *http.Client
}

func newOrchestrator(ratdURL string, mcp *mcpClient, disco *discoverer, agents *agentsClient, subRuns *subagentRunStore, continuations *continuationStore) *orchestrator {
	return &orchestrator{
		ratdURL:       strings.TrimRight(ratdURL, "/"),
		mcp:           mcp,
		disco:         disco,
		agents:        agents,
		subRuns:       subRuns,
		continuations: continuations,
		http:          &http.Client{Timeout: 180 * time.Second},
	}
}

// continuationTimeout is how long chatTurn waits for the UI to confirm
// at the iteration cap before auto-continuing. 60s matches the Stop /
// Continue banner the bundle.js renders.
const continuationTimeout = 60 * time.Second

// chatTurn runs the loop until the model is done. The depth parameter
// counts subagent nesting (0 = top-level user request, 1 = called by
// the top-level agent's agent__X tool, etc.) and is capped at
// maxSubagentDepth. parentConvID lets nested subagent traces be linked
// back to the conversation that triggered them.
func (o *orchestrator) chatTurn(ctx context.Context, sink sseSink, messages []chatMessage, systemOverride, agentID string, depth int, parentConvID string) error {
	if depth > maxSubagentDepth {
		_ = sink.emit("error", map[string]string{
			"error": fmt.Sprintf("subagent depth cap (%d) exceeded — refusing to recurse further", maxSubagentDepth),
		})
		return fmt.Errorf("subagent depth cap exceeded")
	}

	// Resolve the agent (if any). An invalid agentID is non-fatal — we
	// just fall back to defaults and warn through the started event.
	var agent *Agent
	if agentID != "" {
		a, err := o.agents.get(ctx, agentID)
		if err == nil && a != nil {
			agent = a
			systemOverride = a.SystemPrompt
		} else if err != nil {
			_ = sink.emit("warning", map[string]string{
				"warning": "agent " + agentID + " not found, using defaults",
			})
		}
	}

	// Inject / replace the system prompt up front.
	if systemOverride != "" {
		if len(messages) > 0 && messages[0].Role == "system" {
			messages[0].Content = systemOverride
		} else {
			messages = append([]chatMessage{{Role: "system", Content: systemOverride}}, messages...)
		}
	}

	// Filter the MCP tool list by the agent's whitelist if one's selected.
	allTools := o.disco.allTools()
	if agent != nil && !agent.allowsAll() {
		filtered := allTools[:0]
		for _, t := range allTools {
			if agent.allows(t.NSName) {
				filtered = append(filtered, t)
			}
		}
		allTools = filtered
	}
	tools := openAIToolsFromMCP(allTools)

	// Add one tool per subagent so the model can delegate. Skip the
	// subagent dance entirely if we're already at max depth — there's
	// no point exposing tools we can't actually run.
	subagentTools := []openAITool{}
	if agent != nil && depth < maxSubagentDepth {
		subagentTools = o.subagentTools(ctx, agent)
	}
	tools = append(tools, subagentTools...)

	maxIter := defaultMaxIterations
	if agent != nil && agent.MaxIterations > 0 {
		maxIter = agent.MaxIterations
	}

	started := map[string]any{
		"tools_available": len(tools),
		"servers":         summarizeServers(o.disco.list()),
		"depth":           depth,
	}
	if agent != nil {
		started["agent"] = map[string]any{
			"id": agent.ID, "name": agent.Name, "icon": agent.Icon, "color": agent.Color,
			"tools_allowed":  len(tools) - len(subagentTools),
			"subagents":      len(subagentTools),
			"max_iterations": maxIter,
		}
	}
	_ = sink.emit("started", started)

	iter := 0
	for {
		iter++
		// Continuation gate: at the top-level only (subagents don't
		// prompt — they just hit their own cap and return). When we
		// blow past the iteration budget, ask the UI before pushing
		// on. Auto-yes after continuationTimeout.
		if iter > maxIter {
			if depth > 0 || o.continuations == nil {
				break
			}
			_ = sink.emit("continuation_prompt", map[string]any{
				"iteration":   iter - 1,
				"max":         maxIter,
				"timeout_sec": int(continuationTimeout / time.Second),
				"agent_id":    agentID,
			})
			if !o.continuations.wait(ctx, parentConvID, continuationTimeout) {
				_ = sink.emit("done", map[string]string{"finish_reason": "canceled_at_continuation"})
				return nil
			}
			// User (or timeout) said continue — grant another batch.
			_ = sink.emit("continuation_accepted", map[string]any{"granted": maxIter})
			iter = 1
		}
		resp, err := o.callAI(ctx, sink, messages, tools, agent)
		if err != nil {
			_ = sink.emit("error", map[string]string{"error": err.Error()})
			return err
		}
		// Forward the assistant message back to the UI for transcripting
		// — even if there are tool calls, the model may also have content
		// (some models think out loud before calling a tool).
		_ = sink.emit("assistant_message", resp.Message)
		messages = append(messages, resp.Message)

		// If the model wants to call tools, run them and loop.
		if resp.FinishReason == "tool_calls" && len(resp.Message.ToolCalls) > 0 {
			for _, tc := range resp.Message.ToolCalls {
				_ = sink.emit("tool_call", tc)
				var (
					result string
					isErr  bool
				)
				if strings.HasPrefix(tc.Function.Name, agentToolPrefix) {
					result, isErr = o.runSubagentCall(ctx, sink, tc, depth, parentConvID)
				} else {
					result, isErr = o.runToolCall(ctx, tc)
				}
				_ = sink.emit("tool_result", map[string]any{
					"tool_call_id": tc.ID,
					"name":         tc.Function.Name,
					"is_error":     isErr,
					"output":       result,
				})
				messages = append(messages, chatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    result,
				})
			}
			continue
		}

		// finish_reason == "stop" (or anything else terminal) — we're done.
		_ = sink.emit("done", map[string]string{"finish_reason": resp.FinishReason})
		return nil
	}

	// Only reached when a subagent (depth > 0) hits its cap — the
	// continuation prompt is top-level only. Surface the same error
	// the loop used to emit so the parent agent can decide what to do.
	_ = sink.emit("error", map[string]string{
		"error": fmt.Sprintf("max_iterations (%d) exceeded — the model kept calling tools without answering", maxIter),
	})
	return nil
}

// subagentTools builds one tool declaration per agent in agent.Subagents
// that points at an existing, enabled agent. Each tool takes one
// argument — `task` — which the orchestrator hands to the subagent as
// its user message.
func (o *orchestrator) subagentTools(ctx context.Context, parent *Agent) []openAITool {
	if len(parent.Subagents) == 0 {
		return nil
	}
	out := make([]openAITool, 0, len(parent.Subagents))
	for _, subID := range parent.Subagents {
		sub, err := o.agents.get(ctx, subID)
		if err != nil || sub == nil || sub.Disabled {
			continue
		}
		var t openAITool
		t.Type = "function"
		t.Function.Name = agentToolPrefix + sub.ID
		t.Function.Description = fmt.Sprintf("Delegate to subagent %q. %s. Hand it a focused, self-contained task and it will respond with a final answer.",
			sub.Name, sub.Description)
		t.Function.Parameters = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "A focused, self-contained task for the subagent. Provide all the context it needs — it doesn't see this conversation.",
				},
			},
			"required": []string{"task"},
		}
		out = append(out, t)
	}
	return out
}

// runSubagentCall handles an `agent__<id>` tool call by spawning a
// nested chatTurn against the named subagent and returning its final
// assistant content. The nested run uses a traceSink so we can
// (a) pluck the answer out without polluting the parent's SSE stream,
// and (b) persist the full event stream so the parent's "subagent
// returned no content" can actually be debugged.
func (o *orchestrator) runSubagentCall(ctx context.Context, _ sseSink, tc toolCall, depth int, parentConvID string) (string, bool) {
	subID := strings.TrimPrefix(tc.Function.Name, agentToolPrefix)
	var args struct {
		Task string `json:"task"`
	}
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return "invalid subagent arguments JSON: " + err.Error(), true
		}
	}
	if args.Task == "" {
		return "subagent call missing required `task` argument", true
	}

	sub, err := o.agents.get(ctx, subID)
	if err != nil || sub == nil {
		return fmt.Sprintf("unknown subagent %q", subID), true
	}
	if sub.Disabled {
		return fmt.Sprintf("subagent %q is disabled", subID), true
	}

	// Build the trace record. ID encodes parent_conv + parent_tool_call so
	// `cat /data/subagent_runs/{parent_conv}__*.json` lists every
	// subagent invocation from one conversation.
	run := &SubagentRun{
		ID:               parentConvID + "__" + tc.ID,
		ParentConvID:     parentConvID,
		ParentToolCallID: tc.ID,
		SubagentID:       subID,
		Task:             args.Task,
		StartedAt:        time.Now().UTC(),
	}
	if parentConvID == "" {
		// Top-level orphan runs — still save with a placeholder.
		run.ID = "orphan__" + tc.ID
	}

	sink := newTraceSink(run)
	nestedMsgs := []chatMessage{{Role: "user", Content: args.Task}}
	nestedCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	turnErr := o.chatTurn(nestedCtx, sink, nestedMsgs, "", subID, depth+1, parentConvID)
	run.FinishedAt = time.Now().UTC()
	if turnErr != nil {
		run.Error = turnErr.Error()
	}
	run.FinalContent = sink.finalContent()

	// Persist the trace. Best-effort — a save failure shouldn't break
	// the parent's response path.
	if o.subRuns != nil {
		if err := o.subRuns.save(run); err != nil {
			// fall through; the tool result still works.
			_ = err
		}
	}

	if turnErr != nil {
		final := run.FinalContent
		if final == "" {
			final = "subagent error: " + turnErr.Error()
		}
		return final, true
	}
	if run.FinalContent == "" {
		// Surface a hint of what actually happened so the parent can
		// react (and so a human reading the conversation file knows
		// where to look for the full trace).
		hint := fmt.Sprintf("subagent returned no content (trace: /data/subagent_runs/%s.json, events: %d)",
			run.ID, len(run.Events))
		return hint, true
	}
	return run.FinalContent, false
}

// captureSink eats all events from a nested chatTurn and remembers the
// most recent assistant_message content. That's the "final answer" we
// return to the parent as the subagent tool's result.
type captureSink struct {
	lastContent string
}

func (c *captureSink) emit(event string, payload any) error {
	if event != "assistant_message" {
		return nil
	}
	// payload is a chatMessage when emitted from chatTurn; marshal/unmarshal
	// is the laziest way to handle it generically.
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var m chatMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if m.Content != "" {
		c.lastContent = m.Content
	}
	return nil
}

// runToolCall executes a single tool call against the appropriate MCP server.
// Returns the textual result (always non-empty) and an isError flag.
func (o *orchestrator) runToolCall(ctx context.Context, tc toolCall) (string, bool) {
	server, originalName, ok := o.disco.findToolByName(tc.Function.Name)
	if !ok {
		return "unknown tool: " + tc.Function.Name, true
	}
	var args map[string]any
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return "invalid tool arguments JSON: " + err.Error(), true
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	text, isErr, err := o.mcp.callTool(callCtx, server.Capability, originalName, args)
	if err != nil {
		return "tool call failed: " + err.Error(), true
	}
	return text, isErr
}

// ── ai-provider call ───────────────────────────────────────────────

type aiResponse struct {
	Message      chatMessage `json:"message"`
	Model        string      `json:"model"`
	FinishReason string      `json:"finish_reason"`
	Error        string      `json:"error,omitempty"`
}

// callAI streams ai-provider's /chat-with-tools-stream and forwards each
// upstream delta to the orchestrator's sink as "assistant_delta". The
// stream's final "done" event carries the fully assembled assistant
// message — that's what we return for the next loop iteration.
//
// We call ai-provider directly (not through the interconnect broker)
// because the orchestrator is the canonical consumer and the broker's
// envelope would defeat SSE streaming anyway. When agent is non-nil and
// has Model/Temperature set, those are forwarded as overrides so a
// per-agent model swap takes effect.
func (o *orchestrator) callAI(ctx context.Context, sink sseSink, messages []chatMessage, tools []openAITool, agent *Agent) (*aiResponse, error) {
	payload := map[string]any{"messages": messages, "tools": tools}
	if agent != nil {
		if agent.Model != "" {
			payload["model"] = agent.Model
		}
		if agent.Temperature > 0 {
			payload["temperature"] = agent.Temperature
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.ratdURL+"/api/v1/x/ai-provider/chat-with-tools-stream", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai-provider unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return nil, fmt.Errorf("ai-provider HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var ar *aiResponse
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var event, dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// Blank line ends one SSE event — dispatch it.
			if dataLine != "" {
				if e := o.handleAIEvent(sink, event, dataLine, &ar); e != nil {
					return nil, e
				}
			}
			event, dataLine = "", ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(line[6:])
		} else if strings.HasPrefix(line, "data:") {
			dataLine += strings.TrimPrefix(line, "data:")
			dataLine = strings.TrimLeft(dataLine, " ")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read ai stream: %w", err)
	}
	if ar == nil {
		return nil, fmt.Errorf("ai stream ended without a done event")
	}
	if ar.Error != "" {
		return nil, fmt.Errorf("ai-provider: %s", ar.Error)
	}
	return ar, nil
}

// handleAIEvent processes one SSE event from ai-provider. Deltas are
// forwarded to the UI; the "done" event carries the final assembled
// message which we hand back to the loop.
func (o *orchestrator) handleAIEvent(sink sseSink, event, data string, ar **aiResponse) error {
	switch event {
	case "delta":
		// Pass-through; the UI knows how to render content + reasoning.
		var d map[string]any
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return nil // skip malformed
		}
		_ = sink.emit("assistant_delta", d)
	case "done":
		var resp aiResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			return fmt.Errorf("decode done event: %w", err)
		}
		*ar = &resp
	case "error":
		var e struct{ Error string `json:"error"` }
		_ = json.Unmarshal([]byte(data), &e)
		return fmt.Errorf("ai-provider: %s", e.Error)
	}
	return nil
}

// ── Type bridges between MCP and OpenAI's tool format ─────────────

// chatMessage matches the ai-provider's shape so we can pass through.
// We don't import the ai-provider's types — keeping a parallel struct
// keeps the chat plugin standalone.
type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// openAIToolsFromMCP converts the MCP tool list into the OpenAI-style
// `tools` array the ai-provider expects. We use the namespaced name as the
// public tool name so the orchestrator can route the call back to the right
// server when the model picks it.
func openAIToolsFromMCP(mcps []MCPTool) []openAITool {
	out := make([]openAITool, 0, len(mcps))
	for _, m := range mcps {
		var t openAITool
		t.Type = "function"
		t.Function.Name = m.NSName
		t.Function.Description = m.Description
		if m.InputSchema != nil {
			t.Function.Parameters = m.InputSchema
		} else {
			t.Function.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, t)
	}
	return out
}

// summarizeServers gives the UI just the fields it cares about — too much
// detail would clutter the event stream.
func summarizeServers(servers []*MCPServer) []map[string]any {
	out := make([]map[string]any, 0, len(servers))
	for _, s := range servers {
		out = append(out, map[string]any{
			"name":        s.Name,
			"capability":  s.Capability,
			"tools_count": len(s.Tools),
			"error":       s.LastError,
		})
	}
	return out
}
