package main

// ratdClient is the thin HTTP client this plugin uses to read RAT's catalog
// and metadata. The plugin is a pure read-side wrapper — it never mutates
// anything in ratd, so this file stays small.

import (
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
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// listTables returns the catalog's published tables. If ns is empty, all
// namespaces are returned.
func (c *ratdClient) listTables(ctx context.Context, ns string) ([]map[string]any, error) {
	q := ""
	if ns != "" {
		q = "?namespace=" + ns
	}
	raw, err := c.get(ctx, "/api/v1/tables"+q)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tables []map[string]any `json:"tables"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		// Some deployments return a bare array — handle that too.
		var bare []map[string]any
		if err2 := json.Unmarshal(raw, &bare); err2 == nil {
			return bare, nil
		}
		return nil, fmt.Errorf("decode tables: %w", err)
	}
	return resp.Tables, nil
}

// listNamespaces returns the catalog's namespaces.
func (c *ratdClient) listNamespaces(ctx context.Context) ([]map[string]any, error) {
	raw, err := c.get(ctx, "/api/v1/namespaces")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Namespaces []map[string]any `json:"namespaces"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		var bare []map[string]any
		if err2 := json.Unmarshal(raw, &bare); err2 == nil {
			return bare, nil
		}
		return nil, fmt.Errorf("decode namespaces: %w", err)
	}
	return resp.Namespaces, nil
}

// getTable returns the table descriptor (incl. columns) for one table.
func (c *ratdClient) getTable(ctx context.Context, ns, layer, name string) (map[string]any, error) {
	path := fmt.Sprintf("/api/v1/tables/%s/%s/%s", ns, layer, name)
	raw, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode table: %w", err)
	}
	return out, nil
}

// getTableMetadata reshapes the inline description + per-column descriptions
// from GET /tables/{ns}/{layer}/{name} into a focused metadata payload —
// ratd doesn't expose a separate GET metadata endpoint for tables; the
// values are returned alongside the schema. We pull them out so the LLM
// gets just the documentation it asked for, without the row counts and
// type details from get_table_schema.
func (c *ratdClient) getTableMetadata(ctx context.Context, ns, layer, name string) (map[string]any, error) {
	t, err := c.getTable(ctx, ns, layer, name)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"description": t["description"],
	}
	colDescs := map[string]string{}
	if cols, ok := t["columns"].([]any); ok {
		for _, c := range cols {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			name, _ := cm["name"].(string)
			desc, _ := cm["description"].(string)
			if name != "" && desc != "" {
				colDescs[name] = desc
			}
		}
	}
	out["column_descriptions"] = colDescs
	return out, nil
}

func (c *ratdClient) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ratd %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("ratd %s: not found", path)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ratd %s: HTTP %d: %s", path, resp.StatusCode, truncate(string(raw), 200))
	}
	return raw, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
