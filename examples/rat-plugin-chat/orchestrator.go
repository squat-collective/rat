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
const maxIterations = 8

// sseSink is anything that can emit one SSE event with a JSON payload.
type sseSink interface {
	emit(event string, payload any) error
}

// orchestrator wires the AI provider + the MCP discoverer together for one
// turn of the chat.
type orchestrator struct {
	ratdURL string
	mcp     *mcpClient
	disco   *discoverer
	agents  *agentsClient
	http    *http.Client
}

func newOrchestrator(ratdURL string, mcp *mcpClient, disco *discoverer, agents *agentsClient) *orchestrator {
	return &orchestrator{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		mcp:     mcp,
		disco:   disco,
		agents:  agents,
		http:    &http.Client{Timeout: 180 * time.Second},
	}
}

// chatTurn runs the loop until the model is done (or maxIterations is hit).
// messages is the full conversation so far (system + user + prior turns);
// systemOverride, if non-empty, replaces the leading system message.
// agentID, if non-empty, selects an agent: its system_prompt overrides
// systemOverride and its allowed_tools whitelists the tool list shown to
// the LLM.
func (o *orchestrator) chatTurn(ctx context.Context, sink sseSink, messages []chatMessage, systemOverride, agentID string) error {
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

	// Filter the tool list by the agent's whitelist if one's selected.
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

	started := map[string]any{
		"tools_available": len(tools),
		"servers":         summarizeServers(o.disco.list()),
	}
	if agent != nil {
		started["agent"] = map[string]any{
			"id": agent.ID, "name": agent.Name, "icon": agent.Icon,
			"tools_allowed": len(tools),
		}
	}
	_ = sink.emit("started", started)

	for iter := 1; iter <= maxIterations; iter++ {
		resp, err := o.callAI(ctx, sink, messages, tools)
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
				result, isErr := o.runToolCall(ctx, tc)
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

	// Loop limit hit — emit a hint and stop.
	_ = sink.emit("error", map[string]string{
		"error": fmt.Sprintf("max_iterations (%d) exceeded — the model kept calling tools without answering", maxIterations),
	})
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
// envelope would defeat SSE streaming anyway.
func (o *orchestrator) callAI(ctx context.Context, sink sseSink, messages []chatMessage, tools []openAITool) (*aiResponse, error) {
	body, _ := json.Marshal(map[string]any{"messages": messages, "tools": tools})
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
