package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ── OpenAI-format chat-completions types ──────────────────────────
//
// These match the OpenAI chat API, which Ollama (and most LLM servers)
// implement. The plugin talks to whatever OPENAI_BASE_URL points at.

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // a JSON-encoded object
}

type toolDef struct {
	Type     string         `json:"type"`
	Function functionSchema `json:"function"`
}

type functionSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []toolDef     `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// aiClient calls an OpenAI-compatible /chat/completions endpoint.
type aiClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func newAIClient(baseURL, apiKey, model string) *aiClient {
	return &aiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 180 * time.Second},
	}
}

// completeMaxAttempts bounds how many times complete retries a transient
// failure (see attempt).
const completeMaxAttempts = 3

// complete sends a chat-completions request and returns the assistant message.
// A transient failure — typically the LLM server rejecting a malformed tool
// call the model emitted (a 5xx) — is retried, since re-sampling almost always
// produces a valid call.
func (c *aiClient) complete(
	ctx context.Context, messages []chatMessage, tools []toolDef,
) (chatMessage, error) {
	body, err := json.Marshal(chatRequest{Model: c.model, Messages: messages, Tools: tools})
	if err != nil {
		return chatMessage{}, err
	}

	var lastErr error
	for attempt := 1; attempt <= completeMaxAttempts; attempt++ {
		msg, retryable, err := c.attempt(ctx, body)
		if err == nil {
			return msg, nil
		}
		lastErr = err
		if !retryable || attempt == completeMaxAttempts {
			break
		}
		slog.Warn("AI request failed, retrying", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return chatMessage{}, ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return chatMessage{}, lastErr
}

// attempt makes one chat-completions call. retryable is true when the failure
// is transient — a 5xx, which is usually the LLM server rejecting a malformed
// tool call the model emitted, and which re-sampling fixes.
func (c *aiClient) attempt(ctx context.Context, body []byte) (chatMessage, bool, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body),
	)
	if err != nil {
		return chatMessage{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return chatMessage{}, false, fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 500 {
		return chatMessage{}, true,
			fmt.Errorf("the model produced an invalid response (HTTP %d) — try again", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return chatMessage{}, false,
			fmt.Errorf("AI endpoint returned %d: %s", resp.StatusCode, truncateStr(string(raw), 200))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return chatMessage{}, false, fmt.Errorf("decode AI response: %w", err)
	}
	if cr.Error != nil {
		return chatMessage{}, false, fmt.Errorf("AI error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return chatMessage{}, false, fmt.Errorf("AI returned no choices")
	}
	return cr.Choices[0].Message, false, nil
}
