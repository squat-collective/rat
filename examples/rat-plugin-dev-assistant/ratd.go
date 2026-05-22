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

// ratdClient sends chat requests to the AI provider. It prefers the
// interconnect broker (capability "ai.chat") — so the dev-assistant never
// hardcodes the AI plugin — and falls back to a direct ai-provider call if the
// broker is not installed.
type ratdClient struct {
	baseURL string
	http    *http.Client
}

func newRatdClient(baseURL string) *ratdClient {
	return &ratdClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 110 * time.Second},
	}
}

// chat returns the assistant's reply and the model used.
func (c *ratdClient) chat(ctx context.Context, msgs []message) (string, string, error) {
	reply, model, err := c.viaBroker(ctx, msgs)
	if err == nil {
		return reply, model, nil
	}
	// Broker unavailable — fall back to calling the AI provider directly.
	return c.viaProvider(ctx, msgs)
}

// viaBroker routes the request through the interconnect plugin's capability
// broker. It errors only when the broker itself is unavailable.
func (c *ratdClient) viaBroker(ctx context.Context, msgs []message) (string, string, error) {
	payload, _ := json.Marshal(map[string]any{"messages": msgs})
	body, _ := json.Marshal(map[string]any{
		"capability": "ai.chat",
		"payload":    json.RawMessage(payload),
	})
	raw, status, err := c.post(ctx, "/api/v1/x/interconnect/invoke", body)
	if err != nil {
		return "", "", err
	}
	if status != http.StatusOK {
		return "", "", fmt.Errorf("interconnect broker returned %d", status)
	}
	var res struct {
		Body  json.RawMessage `json:"body"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", "", err
	}
	if res.Error != "" {
		return "", "", fmt.Errorf("broker: %s", res.Error)
	}
	return extractReply(res.Body)
}

// viaProvider calls the ai-provider plugin's /chat route directly.
func (c *ratdClient) viaProvider(ctx context.Context, msgs []message) (string, string, error) {
	body, _ := json.Marshal(map[string]any{"messages": msgs})
	raw, status, err := c.post(ctx, "/api/v1/x/ai-provider/chat", body)
	if err != nil {
		return "", "", fmt.Errorf("ai-provider unreachable: %w", err)
	}
	if status != http.StatusOK {
		return "", "", fmt.Errorf("ai-provider returned %d", status)
	}
	return extractReply(raw)
}

// extractReply pulls the assistant message out of an ai-provider /chat response.
func extractReply(raw []byte) (string, string, error) {
	var r struct {
		Message message `json:"message"`
		Model   string  `json:"model"`
		Error   string  `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", "", fmt.Errorf("decode AI response: %w", err)
	}
	if r.Error != "" {
		return "", "", fmt.Errorf("%s", r.Error)
	}
	if strings.TrimSpace(r.Message.Content) == "" {
		return "", "", fmt.Errorf("the AI returned an empty response")
	}
	return r.Message.Content, r.Model, nil
}

func (c *ratdClient) post(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	return raw, resp.StatusCode, nil
}
