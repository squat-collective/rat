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

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
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

// chat sends messages and returns the assistant's reply and the model used.
func (l *llm) chat(ctx context.Context, messages []chatMessage) (chatMessage, string, error) {
	c := l.cfg.get()
	if strings.TrimSpace(c.BaseURL) == "" {
		return chatMessage{}, "", fmt.Errorf("no API base URL configured — set it in the plugin settings")
	}
	if strings.TrimSpace(c.Model) == "" {
		return chatMessage{}, "", fmt.Errorf("no model configured — set it in the plugin settings")
	}

	body, _ := json.Marshal(chatRequest{Model: c.Model, Messages: messages})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatMessage{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := l.http.Do(req)
	if err != nil {
		return chatMessage{}, c.Model, fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

	if resp.StatusCode != http.StatusOK {
		return chatMessage{}, c.Model, fmt.Errorf("AI endpoint returned %d: %s",
			resp.StatusCode, truncate(string(raw), 200))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return chatMessage{}, c.Model, fmt.Errorf("decode AI response: %w", err)
	}
	if cr.Error != nil {
		return chatMessage{}, c.Model, fmt.Errorf("AI error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return chatMessage{}, c.Model, fmt.Errorf("AI returned no choices")
	}
	return cr.Choices[0].Message, c.Model, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
