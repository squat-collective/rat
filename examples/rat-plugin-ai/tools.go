package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// toolSpecs is the tool catalog advertised to the model. The model decides
// when to call them; the plugin executes them against ratd.
var toolSpecs = []toolDef{
	{Type: "function", Function: functionSchema{
		Name: "list_tables",
		Description: "List every data table in the platform (namespace, layer, name). " +
			"This does NOT include row counts or data — use run_query for counts and values.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
	}},
	{Type: "function", Function: functionSchema{
		Name:        "describe_table",
		Description: "Return the column schema of one table.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"namespace":{"type":"string"},"layer":{"type":"string"},"name":{"type":"string"}},` +
			`"required":["namespace","layer","name"]}`),
	}},
	{Type: "function", Function: functionSchema{
		Name: "run_query",
		Description: "Run a read-only DuckDB SQL query and return the rows. " +
			"Reference tables as namespace.layer.name, e.g. default.bronze.orders.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"sql":{"type":"string","description":"a single read-only SELECT query"}},` +
			`"required":["sql"]}`),
	}},
}

// dataTools executes the model's tool calls by calling back into ratd.
type dataTools struct {
	ratdURL string
	http    *http.Client
}

func newDataTools(ratdURL string) *dataTools {
	return &dataTools{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// execute runs a named tool and returns its result as a string for the model.
// Errors are returned as JSON strings so the model can see and recover from them.
func (t *dataTools) execute(ctx context.Context, name, argsJSON string) string {
	switch name {
	case "list_tables":
		return t.get(ctx, "/api/v1/tables")

	case "describe_table":
		var a struct {
			Namespace string `json:"namespace"`
			Layer     string `json:"layer"`
			Name      string `json:"name"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return toolError(err)
		}
		if a.Namespace == "" || a.Layer == "" || a.Name == "" {
			return `{"error":"describe_table requires namespace, layer and name"}`
		}
		return t.get(ctx, fmt.Sprintf("/api/v1/tables/%s/%s/%s",
			url.PathEscape(a.Namespace), url.PathEscape(a.Layer), url.PathEscape(a.Name)))

	case "run_query":
		var a struct {
			SQL string `json:"sql"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return toolError(err)
		}
		if strings.TrimSpace(a.SQL) == "" {
			return `{"error":"run_query requires a sql query"}`
		}
		raw := t.post(ctx, "/api/v1/query", map[string]any{"sql": a.SQL, "limit": 50})
		return cleanQueryResult(raw)

	default:
		return fmt.Sprintf(`{"error":"unknown tool %q"}`, name)
	}
}

func (t *dataTools) get(ctx context.Context, path string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.ratdURL+path, nil)
	if err != nil {
		return toolError(err)
	}
	return t.do(req)
}

func (t *dataTools) post(ctx context.Context, path string, body any) string {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.ratdURL+path, bytes.NewReader(data))
	if err != nil {
		return toolError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return t.do(req)
}

func (t *dataTools) do(req *http.Request) string {
	resp, err := t.http.Do(req)
	if err != nil {
		return toolError(err)
	}
	defer resp.Body.Close()
	// Cap tool output so a huge result can't blow the model's context window.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 48*1024))
	return string(raw)
}

func toolError(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}

// cleanQueryResult reduces the query API response to just {columns, rows}.
// The raw response also has total_rows/duration_ms — and small models can
// confuse total_rows (the result-set size) with a value inside the rows, e.g.
// reading SELECT count(*) as 1 (one result row) instead of the actual count.
func cleanQueryResult(raw string) string {
	var full struct {
		Columns json.RawMessage `json:"columns"`
		Rows    json.RawMessage `json:"rows"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &full); err != nil || full.Error != "" {
		return raw // unrecognised or an error payload — pass through unchanged
	}
	out, _ := json.Marshal(map[string]json.RawMessage{
		"columns": full.Columns,
		"rows":    full.Rows,
	})
	return string(out)
}
