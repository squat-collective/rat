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

// complete sends one chat-completions request and returns the assistant message.
func (c *aiClient) complete(
	ctx context.Context, messages []chatMessage, tools []toolDef,
) (chatMessage, error) {
	body, err := json.Marshal(chatRequest{Model: c.model, Messages: messages, Tools: tools})
	if err != nil {
		return chatMessage{}, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body),
	)
	if err != nil {
		return chatMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return chatMessage{}, fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return chatMessage{}, fmt.Errorf("AI endpoint returned %d: %s", resp.StatusCode, string(raw))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return chatMessage{}, fmt.Errorf("decode AI response: %w", err)
	}
	if cr.Error != nil {
		return chatMessage{}, fmt.Errorf("AI error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return chatMessage{}, fmt.Errorf("AI returned no choices")
	}
	return cr.Choices[0].Message, nil
}
