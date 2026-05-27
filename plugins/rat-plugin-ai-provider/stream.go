package main

// Streaming variant of chat-with-tools. The OpenAI/Ollama protocol uses
// SSE: each `data:` line carries a delta — either content fragments, or
// partial tool_call assembly. The deltas need to be re-assembled into a
// single final assistant message (with stitched-together tool_calls), and
// we forward each delta to the caller so the UI can paint as tokens
// arrive instead of waiting for the whole reply.

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

// streamDelta is the partial update we forward to the caller per upstream
// SSE event. content is the text fragment (may be empty), reasoning is the
// chain-of-thought from reasoning models (e.g. gpt-oss) — kept separate so
// the UI can show it as "thinking..." while waiting for real content,
// tool_calls are partial pieces to merge into a tool_call by index,
// finish_reason marks the end ("stop", "tool_calls").
type streamDelta struct {
	Content      string                `json:"content,omitempty"`
	Reasoning    string                `json:"reasoning,omitempty"`
	ToolCalls    []streamToolCallDelta `json:"tool_calls,omitempty"`
	FinishReason string                `json:"finish_reason,omitempty"`
	Role         string                `json:"role,omitempty"`
}

// hasPayload returns false for deltas that are just role echoes. Reasoning
// models emit dozens of role-only events before any content; we drop those
// to avoid spamming the UI.
func (d streamDelta) hasPayload() bool {
	return d.Content != "" || d.Reasoning != "" || len(d.ToolCalls) > 0 || d.FinishReason != ""
}

// streamToolCallDelta carries the per-event slice of a single tool_call.
// `index` is the position in the assistant's tool_calls list — fragments
// for different tool_calls interleave in the stream, so we key by index.
type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

// streamSink emits one delta to the chat orchestrator (or any caller).
type streamSink interface {
	emit(ev string, v any) error
}

// chatWithToolsStream sends messages with stream: true, parses each delta
// out of the upstream SSE, forwards it through sink, and returns the
// fully-assembled assistant message plus its finish_reason at the end.
func (l *llm) chatWithToolsStream(
	ctx context.Context, messages []chatMessage, tools []toolDeclaration, ov callOverrides, sink streamSink,
) (chatMessage, string, string, error) {
	c := l.cfg.get()
	if strings.TrimSpace(c.BaseURL) == "" {
		return chatMessage{}, "", "", fmt.Errorf("no API base URL configured — set it in the plugin settings")
	}
	model := c.Model
	if ov.Model != "" {
		model = ov.Model
	}
	if strings.TrimSpace(model) == "" {
		return chatMessage{}, "", "", fmt.Errorf("no model configured — set it in the plugin settings")
	}

	reqPayload := map[string]any{
		"model":    model,
		"messages": messages,
		"tools":    tools,
		"stream":   true,
	}
	if ov.Temperature > 0 {
		reqPayload["temperature"] = ov.Temperature
	}
	body, _ := json.Marshal(reqPayload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatMessage{}, "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	// Streaming requests need a longer overall budget — token-by-token can
	// take a while for big answers.
	streamingClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := streamingClient.Do(req)
	if err != nil {
		return chatMessage{}, model, "", fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return chatMessage{}, model, "", fmt.Errorf("AI endpoint returned %d: %s",
			resp.StatusCode, truncate(string(raw), 300))
	}

	// Reassemble state as deltas arrive.
	var (
		contentBuf   strings.Builder
		reasoningBuf strings.Builder
		role         = "assistant"
		toolCalls    = map[int]*toolCall{} // index → partial tool_call
		toolOrder    []int                 // first-seen order so we can return in spec order
		finishReason string
	)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev struct {
			Choices []struct {
				Index        int                   `json:"index"`
				Delta        streamDelta           `json:"delta"`
				FinishReason string                `json:"finish_reason"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			// A malformed event isn't fatal — skip it.
			continue
		}
		if ev.Error != nil {
			return chatMessage{}, model, "", fmt.Errorf("AI error: %s", ev.Error.Message)
		}
		if len(ev.Choices) == 0 {
			continue
		}
		ch := ev.Choices[0]
		if ch.Delta.Role != "" {
			role = ch.Delta.Role
		}
		if ch.Delta.Content != "" {
			contentBuf.WriteString(ch.Delta.Content)
		}
		if ch.Delta.Reasoning != "" {
			reasoningBuf.WriteString(ch.Delta.Reasoning)
		}
		for _, tcd := range ch.Delta.ToolCalls {
			tc, ok := toolCalls[tcd.Index]
			if !ok {
				tc = &toolCall{Type: "function"}
				toolCalls[tcd.Index] = tc
				toolOrder = append(toolOrder, tcd.Index)
			}
			if tcd.ID != "" {
				tc.ID = tcd.ID
			}
			if tcd.Type != "" {
				tc.Type = tcd.Type
			}
			if tcd.Function.Name != "" {
				tc.Function.Name += tcd.Function.Name
			}
			if tcd.Function.Arguments != "" {
				tc.Function.Arguments += tcd.Function.Arguments
			}
		}
		if ch.FinishReason != "" {
			finishReason = ch.FinishReason
		}

		// Forward this raw delta — the chat orchestrator turns it into an
		// "assistant_delta" SSE event for the UI. Skip pure role echoes so
		// reasoning models don't spam dozens of empty events upfront.
		if !ch.Delta.hasPayload() {
			continue
		}
		if err := sink.emit("delta", ch.Delta); err != nil {
			return chatMessage{}, model, "", err
		}
	}
	if err := scanner.Err(); err != nil {
		return chatMessage{}, model, "", fmt.Errorf("read stream: %w", err)
	}

	// Build the final assembled message.
	final := chatMessage{Role: role, Content: contentBuf.String()}
	for _, idx := range toolOrder {
		if tc := toolCalls[idx]; tc != nil {
			final.ToolCalls = append(final.ToolCalls, *tc)
		}
	}
	return final, model, finishReason, nil
}
