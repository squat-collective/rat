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
	"sync"
	"time"
)

// chartRowLimit caps how many rows a chart query may return. Charts with more
// points than this are unreadable anyway, and the cap keeps responses small.
const chartRowLimit = 500

// queryAttempts bounds how many times a failed query is retried.
const queryAttempts = 3

// ratdClient runs chart SQL against ratd's read-only query API.
//
// Queries are *serialised*: ratq executes on a single DuckDB connection that
// is not safe for concurrent use — firing a dashboard's component queries in
// parallel makes them collide ("No open result set"). A short retry also
// covers the rarer case of colliding with another service querying ratq.
type ratdClient struct {
	baseURL string
	http    *http.Client
	mu      sync.Mutex
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

// run executes one read-only SQL query through ratd and returns its rows. It
// holds the client mutex for the whole call, so the plugin never sends ratq
// two queries at once; a transient failure (5xx / network) is retried.
func (c *ratdClient) run(ctx context.Context, sql string) queryResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	var res queryResult
	for attempt := 1; attempt <= queryAttempts; attempt++ {
		var retryable bool
		res, retryable = c.attempt(ctx, sql)
		if res.Error == "" || !retryable || attempt == queryAttempts {
			return res
		}
		slog.Info("query failed, retrying", "attempt", attempt, "error", res.Error)
		select {
		case <-ctx.Done():
			return queryResult{Error: "query cancelled"}
		case <-time.After(time.Duration(attempt) * 150 * time.Millisecond):
		}
	}
	return res
}

// attempt makes one query call. retryable is true for a transient failure
// (a 5xx or a network error) — usually a concurrent-query collision on ratq.
func (c *ratdClient) attempt(ctx context.Context, sql string) (queryResult, bool) {
	body, _ := json.Marshal(map[string]any{"sql": sql, "limit": chartRowLimit})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/query", bytes.NewReader(body))
	if err != nil {
		return queryResult{Error: err.Error()}, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return queryResult{Error: "query service unreachable: " + err.Error()}, true
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

	var parsed struct {
		Rows    []map[string]any `json:"rows"`
		Error   string           `json:"error"`
		Message string           `json:"message"`
	}
	parseErr := json.Unmarshal(raw, &parsed)
	transient := resp.StatusCode >= 500

	if parseErr != nil {
		return queryResult{Error: fmt.Sprintf("unexpected query response (%d): %s",
			resp.StatusCode, truncate(string(raw), 200))}, transient
	}
	if transient {
		return queryResult{Error: firstNonEmpty(parsed.Error, parsed.Message,
			"the query service is busy")}, true
	}
	if parsed.Error != "" {
		return queryResult{Error: parsed.Error}, false
	}
	if resp.StatusCode != http.StatusOK {
		return queryResult{Error: firstNonEmpty(parsed.Message,
			fmt.Sprintf("query API returned status %d", resp.StatusCode))}, false
	}
	return queryResult{Rows: parsed.Rows}, false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
