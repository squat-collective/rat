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

// ── Tool catalog ──────────────────────────────────────────────────
//
// Leaf tools do real work against ratd and are given to the specialist
// sub-agents. Delegation tools are given to the orchestrator, which uses them
// to hand a focused task to a sub-agent.

var (
	specListTables = toolDef{Type: "function", Function: functionSchema{
		Name: "list_tables",
		Description: "List every data table in the platform (namespace, layer, name). " +
			"This does NOT include row counts or data — use run_query for those.",
		Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
	}}

	specDescribeTable = toolDef{Type: "function", Function: functionSchema{
		Name:        "describe_table",
		Description: "Return the column schema of one table.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"namespace":{"type":"string"},"layer":{"type":"string"},"name":{"type":"string"}},` +
			`"required":["namespace","layer","name"]}`),
	}}

	specRunQuery = toolDef{Type: "function", Function: functionSchema{
		Name: "run_query",
		Description: "Run a read-only DuckDB SQL query and return the rows. " +
			"Reference tables as namespace.layer.name, e.g. default.bronze.orders.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"sql":{"type":"string","description":"a single read-only SELECT query"}},` +
			`"required":["sql"]}`),
	}}

	specRenderChart = toolDef{Type: "function", Function: functionSchema{
		Name: "render_chart",
		Description: "Draw a chart from a SQL query and show it to the user in the chat. " +
			"Choose the chart type and styling. value_columns may list several numeric " +
			"columns to plot multiple series.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"chart_type":{"type":"string","enum":["bar","line","area","pie","donut","radar"],` +
			`"description":"donut is a pie with a hole"},` +
			`"title":{"type":"string"},` +
			`"sql":{"type":"string","description":"a SELECT returning a label column and one or more numeric value columns"},` +
			`"label_column":{"type":"string","description":"result column for the category / x-axis labels"},` +
			`"value_columns":{"type":"array","items":{"type":"string"},"description":"one or more numeric result columns to plot as series"},` +
			`"options":{"type":"object","description":"optional chart styling","properties":{` +
			`"palette":{"type":"string","enum":["rat","vivid","ocean","sunset","mono"]},` +
			`"colors":{"type":"array","items":{"type":"string"},"description":"explicit hex colour per series, e.g. #4ade80"},` +
			`"stacked":{"type":"boolean","description":"stack series (bar, area)"},` +
			`"curve":{"type":"string","enum":["smooth","linear","step"],"description":"line/area curve style"},` +
			`"dots":{"type":"boolean","description":"show point markers on a line"},` +
			`"horizontal":{"type":"boolean","description":"horizontal bars"},` +
			`"bar_radius":{"type":"integer","description":"bar corner radius, 0-16"},` +
			`"inner_radius":{"type":"integer","description":"pie donut hole percent, 0-80"},` +
			`"show_labels":{"type":"boolean","description":"draw value labels on the chart"},` +
			`"hide_grid":{"type":"boolean"},"hide_legend":{"type":"boolean"}}}},` +
			`"required":["chart_type","title","sql","label_column","value_columns"]}`),
	}}

	// Delegation tools — the orchestrator hands a task to a specialist sub-agent.
	specQueryData = toolDef{Type: "function", Function: functionSchema{
		Name: "query_data",
		Description: "Ask the data specialist a question about the data — tables, schemas, " +
			"row counts, values, comparisons, any analysis. Pass the full question in " +
			"plain English; the specialist inspects schemas and runs the queries.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"question":{"type":"string","description":"the full data question in plain English"}},` +
			`"required":["question"]}`),
	}}

	specCreateChart = toolDef{Type: "function", Function: functionSchema{
		Name: "create_chart",
		Description: "Ask the visualisation specialist to draw a chart for the user. " +
			"Describe what to visualise in plain English — what data and which kind of chart.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"request":{"type":"string","description":"what to visualise, in plain English"}},` +
			`"required":["request"]}`),
	}}
)

// Tool sets per agent.
var (
	orchestratorTools = []toolDef{specQueryData, specCreateChart}
	sqlAgentTools     = []toolDef{specListTables, specDescribeTable, specRunQuery}
	chartAgentTools   = []toolDef{specListTables, specDescribeTable, specRunQuery, specRenderChart}
)

// chartSpec is a graph the model drew. It carries the chart's configuration
// and its data rows: the chat renders it (with the charts plugin's renderer
// when available) and "pin to dashboard" turns it into a live chart component.
type chartSpec struct {
	Title    string           `json:"title"`
	Type     string           `json:"type"` // bar | line | area | pie | radar
	XColumn  string           `json:"x_column"`
	YColumns []string         `json:"y_columns"`
	Options  json.RawMessage  `json:"options,omitempty"`
	SQL      string           `json:"sql"`
	Rows     []map[string]any `json:"rows"`
}

// aiChartTypes are the chart types render_chart accepts. An unknown type falls
// back to "bar"; "donut" is handled as a pie before this map is consulted.
var aiChartTypes = map[string]bool{
	"bar": true, "line": true, "area": true, "pie": true, "radar": true,
}

// dataTools executes the leaf tool calls by calling back into ratd.
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

// execute runs a leaf tool. It returns a string result for the model and, for
// render_chart, a chart spec for the UI (nil otherwise). Errors are returned as
// JSON strings so the model can see and recover from them.
func (t *dataTools) execute(ctx context.Context, name, argsJSON string) (string, *chartSpec) {
	switch name {
	case "list_tables":
		return t.get(ctx, "/api/v1/tables"), nil

	case "describe_table":
		var a struct {
			Namespace string `json:"namespace"`
			Layer     string `json:"layer"`
			Name      string `json:"name"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return toolError(err), nil
		}
		if a.Namespace == "" || a.Layer == "" || a.Name == "" {
			return `{"error":"describe_table requires namespace, layer and name"}`, nil
		}
		return t.get(ctx, fmt.Sprintf("/api/v1/tables/%s/%s/%s",
			url.PathEscape(a.Namespace), url.PathEscape(a.Layer), url.PathEscape(a.Name))), nil

	case "run_query":
		var a struct {
			SQL string `json:"sql"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return toolError(err), nil
		}
		if strings.TrimSpace(a.SQL) == "" {
			return `{"error":"run_query requires a sql query"}`, nil
		}
		return cleanQueryResult(t.post(ctx, "/api/v1/query",
			map[string]any{"sql": a.SQL, "limit": 50})), nil

	case "render_chart":
		return t.renderChart(ctx, argsJSON)

	default:
		return fmt.Sprintf(`{"error":"unknown tool %q"}`, name), nil
	}
}

// renderChart runs the chart's SQL and builds a chartSpec — the chart's config
// plus its data rows — which the chat renders and can pin to a dashboard. The
// options object is carried through verbatim.
func (t *dataTools) renderChart(ctx context.Context, argsJSON string) (string, *chartSpec) {
	var a struct {
		ChartType    string          `json:"chart_type"`
		Title        string          `json:"title"`
		SQL          string          `json:"sql"`
		LabelColumn  string          `json:"label_column"`
		ValueColumns []string        `json:"value_columns"`
		ValueColumn  string          `json:"value_column"` // tolerated singular form
		Options      json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return toolError(err), nil
	}
	yCols := cleanCols(a.ValueColumns)
	if len(yCols) == 0 && strings.TrimSpace(a.ValueColumn) != "" {
		yCols = []string{strings.TrimSpace(a.ValueColumn)}
	}
	if strings.TrimSpace(a.SQL) == "" || strings.TrimSpace(a.LabelColumn) == "" || len(yCols) == 0 {
		return `{"error":"render_chart requires sql, label_column and value_columns"}`, nil
	}
	ctype := strings.ToLower(strings.TrimSpace(a.ChartType))
	if ctype == "donut" {
		// "donut" is a pie with a hole — map it and ensure there is one.
		ctype = "pie"
		a.Options = ensureDonut(a.Options)
	}
	if !aiChartTypes[ctype] {
		ctype = "bar"
	}

	raw := t.post(ctx, "/api/v1/query", map[string]any{"sql": a.SQL, "limit": 80})
	var resp struct {
		Rows  []map[string]any `json:"rows"`
		Error string           `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return fmt.Sprintf(`{"error":"chart query failed: %s"}`, truncateStr(raw, 300)), nil
	}
	if resp.Error != "" {
		return raw, nil
	}
	if len(resp.Rows) == 0 {
		return `{"error":"the chart query returned no rows"}`, nil
	}
	// Verify the named columns exist in the result.
	first := resp.Rows[0]
	if _, ok := first[a.LabelColumn]; !ok {
		return fmt.Sprintf(`{"error":"label_column %q is not in the query result"}`, a.LabelColumn), nil
	}
	for _, y := range yCols {
		if _, ok := first[y]; !ok {
			return fmt.Sprintf(`{"error":"value column %q is not in the query result"}`, y), nil
		}
	}

	spec := &chartSpec{
		Title: a.Title, Type: ctype, XColumn: a.LabelColumn,
		YColumns: yCols, Options: a.Options, SQL: a.SQL, Rows: resp.Rows,
	}
	summary, _ := json.Marshal(map[string]any{
		"status": "chart drawn and shown to the user",
		"type":   ctype,
		"title":  a.Title,
		"series": yCols,
		"rows":   len(resp.Rows),
	})
	return string(summary), spec
}

// cleanCols trims and drops empty entries from a column-name slice.
func cleanCols(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ensureDonut guarantees a pie chart has a donut hole — used when the model
// asks for a "donut" but leaves inner_radius unset.
func ensureDonut(raw json.RawMessage) json.RawMessage {
	m := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	if v, ok := m["inner_radius"]; !ok || v == nil || v == float64(0) {
		m["inner_radius"] = 55
	}
	out, _ := json.Marshal(m)
	return out
}

// ── ratd HTTP helpers ─────────────────────────────────────────────

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
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	return string(raw)
}

func toolError(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
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
