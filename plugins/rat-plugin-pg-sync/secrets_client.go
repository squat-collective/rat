package main

// secretsClient resolves a secret name to a plaintext value by going
// through the interconnect broker. We never call the secrets plugin
// directly — the broker is the contract.

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

type secretsClient struct {
	ratdURL string
	http    *http.Client
}

func newSecretsClient(ratdURL string) *secretsClient {
	return &secretsClient{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Resolve asks the broker for the named secret. The broker forwards to
// whichever plugin registered `secrets.get` and wraps the response as
// `{capability, provider, status, body}`. We unwrap and return body.value
// (the actual plaintext) or an error built from body.error.
func (s *secretsClient) Resolve(ctx context.Context, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"capability": "secrets.get",
		"payload":    map[string]string{"name": name},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.ratdURL+"/api/v1/x/interconnect/invoke", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("broker unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("broker HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var wrap struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return "", fmt.Errorf("decode broker envelope: %w", err)
	}
	if wrap.Status >= 300 {
		var bodyErr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(wrap.Body, &bodyErr)
		if bodyErr.Error != "" {
			return "", fmt.Errorf("secrets.get(%q): %s", name, bodyErr.Error)
		}
		return "", fmt.Errorf("secrets.get(%q): HTTP %d", name, wrap.Status)
	}
	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(wrap.Body, &payload); err != nil {
		return "", fmt.Errorf("decode secret payload: %w", err)
	}
	if payload.Value == "" {
		return "", fmt.Errorf("secret %q resolved to an empty value", name)
	}
	return payload.Value, nil
}
