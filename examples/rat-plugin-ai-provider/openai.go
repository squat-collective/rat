package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── OpenAI-format chat-completions types ──────────────────────────

// chatMessage is the OpenAI message shape — with optional fields for the
// tool-use loop. role is one of "system", "user", "assistant", "tool".
// When the assistant decides to call tools, its message has tool_calls
// populated and content is empty; tool results come back as role="tool"
// messages with the matching tool_call_id.
type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// toolCall is the assistant's request to invoke one of the tools declared
// in the request. Arguments is a JSON string (OpenAI's choice — the model
// emits JSON inside a string).
type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // always "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// toolDeclaration is what the caller sends to the model — the schema the
// model uses to decide whether to call this tool and what to pass.
type toolDeclaration struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type chatRequest struct {
	Model       string            `json:"model"`
	Messages    []chatMessage     `json:"messages"`
	Tools       []toolDeclaration `json:"tools,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// llm calls an OpenAI-compatible /chat/completions endpoint using whatever
// config is current — so a config change in the portal takes effect on the
// next call, with no restart.
type llm struct {
	cfg  *configStore
	http *http.Client
}

func newLLM(cfg *configStore) *llm {
	return &llm{cfg: cfg, http: &http.Client{Timeout: 100 * time.Second}}
}

// callOverrides bundles per-call overrides the caller can pass. Both
// model and temperature are optional; empty/zero means "use the
// plugin's configured default". This is what lets agents in
// rat-plugin-agents pick their own model / temperature without
// affecting other consumers.
type callOverrides struct {
	Model       string
	Temperature float64 // 0 = leave unset (don't send to upstream)
}

// chat sends messages and returns the assistant's reply and the model used.
func (l *llm) chat(ctx context.Context, messages []chatMessage) (chatMessage, string, error) {
	msg, model, _, err := l.chatWithTools(ctx, messages, nil, callOverrides{})
	return msg, model, err
}

// chatWithTools is the tool-aware variant: pass the tools the model can
// call, and inspect FinishReason / message.ToolCalls in the response.
// Both chat() and chatWithTools() hit the same /chat/completions endpoint
// — OpenAI's API multiplexes tool-use into the same request format.
func (l *llm) chatWithTools(
	ctx context.Context, messages []chatMessage, tools []toolDeclaration, ov callOverrides,
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

	reqBody := chatRequest{Model: model, Messages: messages, Tools: tools}
	if ov.Temperature > 0 {
		t := ov.Temperature
		reqBody.Temperature = &t
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatMessage{}, "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := l.http.Do(req)
	if err != nil {
		return chatMessage{}, model, "", fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))

	if resp.StatusCode != http.StatusOK {
		return chatMessage{}, model, "", fmt.Errorf("AI endpoint returned %d: %s",
			resp.StatusCode, truncate(string(raw), 200))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return chatMessage{}, model, "", fmt.Errorf("decode AI response: %w", err)
	}
	if cr.Error != nil {
		return chatMessage{}, model, "", fmt.Errorf("AI error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return chatMessage{}, model, "", fmt.Errorf("AI returned no choices")
	}
	return cr.Choices[0].Message, model, cr.Choices[0].FinishReason, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
