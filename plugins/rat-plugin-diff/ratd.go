package main

// ratdClient is the read-only HTTP wrapper the poller uses to discover
// plugins, pipelines, schedules, runs, namespaces, tables. Plus the
// query endpoint for the iceberg snapshot + row-diff drill-in.

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
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

type pluginListItem struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Status     string          `json:"status"`
	Healthy    bool            `json:"healthy"`
	Config     json.RawMessage `json:"config"`
	Descriptor json.RawMessage `json:"descriptor"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func (c *ratdClient) ListPlugins(ctx context.Context) ([]pluginListItem, error) {
	var resp struct {
		Plugins []pluginListItem `json:"plugins"`
	}
	// /api/v1/plugins returns either a bare array or {plugins:[]} — try
	// both. The bare-array form is what the agents catalog returns.
	raw, status, err := c.get(ctx, "/api/v1/plugins")
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("list plugins: HTTP %d", status)
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Plugins) == 0 {
		var arr []pluginListItem
		if err2 := json.Unmarshal(raw, &arr); err2 == nil {
			return arr, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return resp.Plugins, nil
}

type pipelineItem struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Name      string `json:"name"`
	Type      string `json:"type"`
}

func (c *ratdClient) ListPipelines(ctx context.Context) ([]pipelineItem, error) {
	raw, status, err := c.get(ctx, "/api/v1/pipelines")
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("list pipelines: HTTP %d", status)
	}
	var wrap struct {
		Pipelines []pipelineItem `json:"pipelines"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Pipelines) >= 0 {
		return wrap.Pipelines, nil
	}
	var arr []pipelineItem
	_ = json.Unmarshal(raw, &arr)
	return arr, nil
}

type scheduleItem struct {
	ID         string `json:"id"`
	PipelineID string `json:"pipeline_id"`
	Cron       string `json:"cron"`
	Enabled    bool   `json:"enabled"`
}

func (c *ratdClient) ListSchedules(ctx context.Context) ([]scheduleItem, error) {
	raw, status, err := c.get(ctx, "/api/v1/schedules")
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("list schedules: HTTP %d", status)
	}
	var wrap struct {
		Schedules []scheduleItem `json:"schedules"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Schedules != nil {
		return wrap.Schedules, nil
	}
	var arr []scheduleItem
	_ = json.Unmarshal(raw, &arr)
	return arr, nil
}

type secretItem struct {
	Name        string    `json:"name"`
	UpdatedAt   time.Time `json:"updated_at"`
	Description string    `json:"description"`
}

// ListSecrets is best-effort — the secrets plugin may not be deployed.
// We swallow the 404/connect-error and return an empty list rather than
// fail the whole poll.
func (c *ratdClient) ListSecrets(ctx context.Context) []secretItem {
	raw, status, err := c.get(ctx, "/api/v1/x/secrets/secrets")
	if err != nil || status >= 300 {
		return nil
	}
	var wrap struct {
		Secrets []secretItem `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil
	}
	return wrap.Secrets
}

type runItem struct {
	ID          string    `json:"id"`
	PipelineID  string    `json:"pipeline_id"`
	Status      string    `json:"status"`
	Trigger     string    `json:"trigger"`
	RowsWritten int64     `json:"rows_written"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

// ListRecentRuns returns the most-recent runs (default limit 50). Used
// to spot newly-completed runs since the last poll.
func (c *ratdClient) ListRecentRuns(ctx context.Context, limit int) ([]runItem, error) {
	raw, status, err := c.get(ctx, fmt.Sprintf("/api/v1/runs?limit=%d", limit))
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("list runs: HTTP %d", status)
	}
	var wrap struct {
		Runs []runItem `json:"runs"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Runs != nil {
		return wrap.Runs, nil
	}
	var arr []runItem
	_ = json.Unmarshal(raw, &arr)
	return arr, nil
}

type namespaceItem struct {
	Name string `json:"name"`
}

func (c *ratdClient) ListNamespaces(ctx context.Context) ([]namespaceItem, error) {
	raw, status, err := c.get(ctx, "/api/v1/namespaces")
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("list namespaces: HTTP %d", status)
	}
	var wrap struct {
		Namespaces []namespaceItem `json:"namespaces"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Namespaces != nil {
		return wrap.Namespaces, nil
	}
	var arr []namespaceItem
	_ = json.Unmarshal(raw, &arr)
	return arr, nil
}

type tableItem struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Name      string `json:"name"`
	RowCount  int64  `json:"row_count"`
}

func (c *ratdClient) ListTables(ctx context.Context) ([]tableItem, error) {
	raw, status, err := c.get(ctx, "/api/v1/tables")
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("list tables: HTTP %d", status)
	}
	var wrap struct {
		Tables []tableItem `json:"tables"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Tables != nil {
		return wrap.Tables, nil
	}
	var arr []tableItem
	_ = json.Unmarshal(raw, &arr)
	return arr, nil
}

// QueryResult is the shape ratd returns from POST /api/v1/query.
type QueryResult struct {
	Columns []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"columns"`
	Rows []map[string]any `json:"rows"`
}

func (c *ratdClient) Query(ctx context.Context, sql string) (*QueryResult, error) {
	body, _ := json.Marshal(map[string]string{"sql": sql})
	raw, status, err := c.postJSON(ctx, "/api/v1/query", body)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		var errResp struct {
			Error struct{ Message string } `json:"error"`
		}
		_ = json.Unmarshal(raw, &errResp)
		if errResp.Error.Message != "" {
			return nil, fmt.Errorf("query failed: %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("query: HTTP %d: %s", status, string(raw))
	}
	var qr QueryResult
	if err := json.Unmarshal(raw, &qr); err != nil {
		return nil, fmt.Errorf("decode query: %w", err)
	}
	return &qr, nil
}

func (c *ratdClient) get(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	return raw, resp.StatusCode, nil
}

func (c *ratdClient) postJSON(ctx context.Context, path string, body []byte) ([]byte, int, error) {
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
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	return raw, resp.StatusCode, nil
}
