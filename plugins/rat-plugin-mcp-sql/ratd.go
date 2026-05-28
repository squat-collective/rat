package main

// ratdClient is the small HTTP client this plugin uses to run queries
// against ratd's read-only /api/v1/query endpoint (which itself goes to
// ratq + DuckDB). The plugin is read-only — it never mutates anything.

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

type ratdClient struct {
	baseURL string
	http    *http.Client
}

func newRatdClient(baseURL string) *ratdClient {
	return &ratdClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// QueryResult is the shape /api/v1/query returns.
type QueryResult struct {
	Columns    []map[string]any `json:"columns"`
	Rows       []map[string]any `json:"rows"`
	TotalRows  int              `json:"total_rows"`
	DurationMs int              `json:"duration_ms"`
}

// executeQuery posts SQL to ratd. limit lets the LLM cap rows without
// rewriting the SQL — useful when it forgets a LIMIT.
func (c *ratdClient) executeQuery(ctx context.Context, sql string, limit int) (*QueryResult, error) {
	body, _ := json.Marshal(map[string]any{"sql": sql, "limit": limit})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ratd /query: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if resp.StatusCode >= 300 {
		// ratd returns the DuckDB error message in the body — pass it
		// through so the LLM can fix the query.
		return nil, fmt.Errorf("query failed (HTTP %d): %s", resp.StatusCode, truncate(string(raw), 500))
	}
	var qr QueryResult
	if err := json.Unmarshal(raw, &qr); err != nil {
		return nil, fmt.Errorf("decode query result: %w", err)
	}
	return &qr, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
