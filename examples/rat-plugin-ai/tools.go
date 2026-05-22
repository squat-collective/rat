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
		Description: "Draw a bar or line chart from a SQL query, show it to the user, and " +
			"save it to the Dashboards plugin. Provide the chart type, a title, a SELECT " +
			"query, and which result columns hold the category labels and the numeric " +
			"values. Returns a chart_id you can pass to save_dashboard.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"chart_type":{"type":"string","enum":["bar","line"]},` +
			`"title":{"type":"string"},` +
			`"sql":{"type":"string","description":"a SELECT returning a label column and a numeric value column"},` +
			`"label_column":{"type":"string","description":"result column for the x-axis labels"},` +
			`"value_column":{"type":"string","description":"result column for the numeric y values"}},` +
			`"required":["chart_type","title","sql","label_column","value_column"]}`),
	}}

	specSaveDashboard = toolDef{Type: "function", Function: functionSchema{
		Name: "save_dashboard",
		Description: "Arrange one or more already-rendered charts into a saved dashboard in " +
			"the portal's Dashboards page. Call this after render_chart when the user wants " +
			"a dashboard. Pass a title and the chart_id values render_chart returned.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"title":{"type":"string"},` +
			`"chart_ids":{"type":"array","items":{"type":"string"},` +
			`"description":"chart_id values returned by earlier render_chart calls"}},` +
			`"required":["title","chart_ids"]}`),
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
		Description: "Ask the visualisation specialist to draw chart(s) — and, when the user " +
			"asks for a dashboard, arrange them into one. Describe what to visualise in " +
			"plain English: what data, which charts, and whether to build a dashboard.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"request":{"type":"string","description":"what to visualise, in plain English"}},` +
			`"required":["request"]}`),
	}}
)

// Tool sets per agent.
var (
	orchestratorTools = []toolDef{specQueryData, specCreateChart}
	sqlAgentTools     = []toolDef{specListTables, specDescribeTable, specRunQuery}
	chartAgentTools   = []toolDef{
		specListTables, specDescribeTable, specRunQuery, specRenderChart, specSaveDashboard,
	}
)

// chartSpec is a chart the model asked to render — returned to the UI. When the
// charts plugin is reachable the chart is also persisted there and ChartID is
// set, so the UI can link to it in the Dashboards page.
type chartSpec struct {
	Type    string    `json:"type"` // "bar" | "line"
	Title   string    `json:"title"`
	Labels  []string  `json:"labels"`
	Values  []float64 `json:"values"`
	ChartID string    `json:"chart_id,omitempty"`
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

// execute runs a leaf tool. It returns a string result for the model and,
// for render_chart, a chart spec for the UI (nil otherwise). Errors are
// returned as JSON strings so the model can see and recover from them.
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

	case "save_dashboard":
		return t.saveDashboard(ctx, argsJSON), nil

	default:
		return fmt.Sprintf(`{"error":"unknown tool %q"}`, name), nil
	}
}

// renderChart runs the chart's SQL, shapes the result into a chartSpec for the
// UI, and returns the data to the model so it can describe the chart.
func (t *dataTools) renderChart(ctx context.Context, argsJSON string) (string, *chartSpec) {
	var a struct {
		ChartType   string `json:"chart_type"`
		Title       string `json:"title"`
		SQL         string `json:"sql"`
		LabelColumn string `json:"label_column"`
		ValueColumn string `json:"value_column"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return toolError(err), nil
	}
	if strings.TrimSpace(a.SQL) == "" || a.LabelColumn == "" || a.ValueColumn == "" {
		return `{"error":"render_chart requires sql, label_column and value_column"}`, nil
	}

	raw := t.post(ctx, "/api/v1/query", map[string]any{"sql": a.SQL, "limit": 50})
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

	spec := &chartSpec{Type: "bar", Title: a.Title}
	if a.ChartType == "line" {
		spec.Type = "line"
	}
	for _, row := range resp.Rows {
		lv, hasL := row[a.LabelColumn]
		vv, hasV := row[a.ValueColumn]
		if !hasL || !hasV {
			return fmt.Sprintf(`{"error":"columns %q / %q are not both in the query result"}`,
				a.LabelColumn, a.ValueColumn), nil
		}
		spec.Labels = append(spec.Labels, fmt.Sprint(lv))
		f, _ := vv.(float64) // JSON numbers decode to float64
		spec.Values = append(spec.Values, f)
	}
	if len(spec.Values) == 0 {
		return `{"error":"the chart query returned no rows"}`, nil
	}

	// Persist the chart in the charts ("Dashboards") plugin so it survives the
	// chat and can be put on a dashboard. If that plugin is not installed the
	// chart still renders inline in the chat — ChartID is simply left empty.
	note := "shown in the chat only — the Dashboards plugin is not available"
	if id, err := t.registerChart(ctx, a.Title, spec.Type, a.SQL, a.LabelColumn, a.ValueColumn); err == nil {
		spec.ChartID = id
		note = "saved to the Dashboards plugin — pass this chart_id to save_dashboard to put it on a dashboard"
	}

	summary, _ := json.Marshal(map[string]any{
		"status":   "chart rendered for the user",
		"chart_id": spec.ChartID,
		"type":     spec.Type,
		"title":    spec.Title,
		"labels":   spec.Labels,
		"values":   spec.Values,
		"note":     note,
	})
	return string(summary), spec
}

// registerChart saves a chart in the charts plugin (proxied by ratd at
// /api/v1/x/charts) so it persists in the portal's Dashboards page. It returns
// the new chart's ID, or an error if the charts plugin is not reachable.
func (t *dataTools) registerChart(ctx context.Context, title, ctype, sql, labelCol, valueCol string) (string, error) {
	raw := t.post(ctx, "/api/v1/x/charts/charts", map[string]any{
		"title":     title,
		"type":      ctype,
		"sql":       sql,
		"x_column":  labelCol,
		"y_columns": []string{valueCol},
	})
	var resp struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return "", fmt.Errorf("charts plugin unavailable")
	}
	if resp.Error != "" {
		return "", fmt.Errorf("charts plugin: %s", resp.Error)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("charts plugin returned no chart id")
	}
	return resp.ID, nil
}

// saveDashboard arranges already-rendered charts into a dashboard in the charts
// plugin and returns its portal URL for the model to relay to the user.
func (t *dataTools) saveDashboard(ctx context.Context, argsJSON string) string {
	var a struct {
		Title    string   `json:"title"`
		ChartIDs []string `json:"chart_ids"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return toolError(err)
	}
	if strings.TrimSpace(a.Title) == "" || len(a.ChartIDs) == 0 {
		return `{"error":"save_dashboard requires a title and at least one chart_id"}`
	}
	widgets := make([]map[string]any, 0, len(a.ChartIDs))
	for _, id := range a.ChartIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		widgets = append(widgets, map[string]any{"chart_id": id, "width": 2, "height": 1})
	}
	if len(widgets) == 0 {
		return `{"error":"no valid chart_ids — call render_chart first and use the ids it returns"}`
	}

	raw := t.post(ctx, "/api/v1/x/charts/dashboards",
		map[string]any{"title": a.Title, "widgets": widgets})
	var resp struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil || resp.ID == "" {
		return `{"error":"could not create the dashboard — the Dashboards plugin may be unavailable"}`
	}
	out, _ := json.Marshal(map[string]string{
		"status": "dashboard created",
		"title":  a.Title,
		"url":    "/x/charts/d/" + resp.ID,
	})
	return string(out)
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
