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

// chartRowLimit caps how many rows a chart query may return. Charts with more
// points than this are unreadable anyway, and the cap keeps responses small.
const chartRowLimit = 500

// ratdClient runs chart SQL against ratd's read-only query API. This is the
// "live" half of the plugin: every chart view re-runs its query here.
type ratdClient struct {
	baseURL string
	http    *http.Client
}

func newRatdClient(baseURL string) *ratdClient {
	return &ratdClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// queryResult is a shaped query response — rows ready for the UI to feed
// straight into Recharts. On failure Error is set and Rows is nil.
type queryResult struct {
	Rows  []map[string]any `json:"rows"`
	Error string           `json:"error,omitempty"`
}

// run executes a single read-only SQL query through ratd and returns its rows.
// Errors are returned in the result rather than as a Go error so a broken
// chart degrades gracefully in the UI instead of failing the whole dashboard.
func (c *ratdClient) run(ctx context.Context, sql string) queryResult {
	body, _ := json.Marshal(map[string]any{"sql": sql, "limit": chartRowLimit})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/query", bytes.NewReader(body))
	if err != nil {
		return queryResult{Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return queryResult{Error: "query service unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

	var parsed struct {
		Rows  []map[string]any `json:"rows"`
		Error string           `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return queryResult{Error: fmt.Sprintf("unexpected query response (%d): %s",
			resp.StatusCode, truncate(string(raw), 200))}
	}
	if parsed.Error != "" {
		return queryResult{Error: parsed.Error}
	}
	if resp.StatusCode != http.StatusOK {
		return queryResult{Error: fmt.Sprintf("query API returned status %d", resp.StatusCode)}
	}
	return queryResult{Rows: parsed.Rows}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
