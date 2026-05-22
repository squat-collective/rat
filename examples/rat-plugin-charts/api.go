package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// api serves the charts / dashboards / reports REST API. ratd proxies it at
// /api/v1/x/charts/* — the proxy forwards the whole path suffix as a wildcard,
// so every route registered here is reachable.
type api struct {
	store *store
	ratd  *ratdClient
}

func newAPI(s *store, rc *ratdClient) *api {
	return &api{store: s, ratd: rc}
}

var chartTypes = map[string]bool{"bar": true, "line": true, "area": true, "pie": true}

// mux wires every REST route. Go 1.22+ ServeMux supports method + {id}
// patterns, which keeps the routing table flat and readable.
func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()

	// Charts — live chart definitions.
	m.HandleFunc("POST /charts", a.createChart)
	m.HandleFunc("GET /charts", a.listCharts)
	m.HandleFunc("GET /charts/{id}", a.getChart)
	m.HandleFunc("GET /charts/{id}/data", a.getChartData)
	m.HandleFunc("DELETE /charts/{id}", a.deleteChart)

	// Dashboards — modular grids of chart widgets.
	m.HandleFunc("POST /dashboards", a.createDashboard)
	m.HandleFunc("GET /dashboards", a.listDashboards)
	m.HandleFunc("GET /dashboards/{id}", a.getDashboard)
	m.HandleFunc("PATCH /dashboards/{id}", a.updateDashboard)
	m.HandleFunc("POST /dashboards/{id}/widgets", a.addWidget)
	m.HandleFunc("DELETE /dashboards/{id}", a.deleteDashboard)

	// Reports — narrative documents interleaving text and charts.
	m.HandleFunc("POST /reports", a.createReport)
	m.HandleFunc("GET /reports", a.listReports)
	m.HandleFunc("GET /reports/{id}", a.getReport)
	m.HandleFunc("DELETE /reports/{id}", a.deleteReport)

	// preview runs ad-hoc SQL without saving — used by the chart editor.
	m.HandleFunc("POST /preview", a.preview)

	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// ── Charts ────────────────────────────────────────────────────────

type chartInput struct {
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	SQL      string   `json:"sql"`
	XColumn  string   `json:"x_column"`
	YColumns []string `json:"y_columns"`
}

func (a *api) createChart(w http.ResponseWriter, r *http.Request) {
	var in chartInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	in.SQL = strings.TrimSpace(in.SQL)
	in.XColumn = strings.TrimSpace(in.XColumn)
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	if in.Type == "" {
		in.Type = "bar"
	}
	if !chartTypes[in.Type] {
		writeErr(w, http.StatusBadRequest, "type must be one of: bar, line, area, pie")
		return
	}
	ys := cleanStrings(in.YColumns)
	if in.Title == "" || in.SQL == "" || in.XColumn == "" || len(ys) == 0 {
		writeErr(w, http.StatusBadRequest, "title, sql, x_column and y_columns are required")
		return
	}
	c := a.store.createChart(&Chart{
		Title: in.Title, Type: in.Type, SQL: in.SQL,
		XColumn: in.XColumn, YColumns: ys,
	})
	writeJSON(w, http.StatusCreated, c)
}

func (a *api) listCharts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.listCharts())
}

func (a *api) getChart(w http.ResponseWriter, r *http.Request) {
	c, ok := a.store.getChart(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "chart not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// chartData is a chart plus the rows from a fresh run of its SQL.
type chartData struct {
	Chart *Chart           `json:"chart"`
	Rows  []map[string]any `json:"rows"`
	Error string           `json:"error,omitempty"`
}

// getChartData re-runs the chart's query and returns its current rows. A query
// failure is reported in the body with HTTP 200 so one broken chart never
// breaks the dashboard or report it sits in.
func (a *api) getChartData(w http.ResponseWriter, r *http.Request) {
	c, ok := a.store.getChart(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "chart not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	res := a.ratd.run(ctx, c.SQL)
	writeJSON(w, http.StatusOK, chartData{Chart: c, Rows: res.Rows, Error: res.Error})
}

func (a *api) deleteChart(w http.ResponseWriter, r *http.Request) {
	if !a.store.deleteChart(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "chart not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Dashboards ────────────────────────────────────────────────────

type dashboardInput struct {
	Title   string   `json:"title"`
	Widgets []Widget `json:"widgets"`
}

func (a *api) createDashboard(w http.ResponseWriter, r *http.Request) {
	var in dashboardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	d := a.store.createDashboard(&Dashboard{
		Title:   in.Title,
		Widgets: normalizeWidgets(in.Widgets),
	})
	writeJSON(w, http.StatusCreated, d)
}

func (a *api) listDashboards(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.listDashboards())
}

func (a *api) getDashboard(w http.ResponseWriter, r *http.Request) {
	d, ok := a.store.getDashboard(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// dashboardPatch updates a dashboard. Absent fields (nil) are left unchanged,
// so a client can rename without resending the whole widget list.
type dashboardPatch struct {
	Title   *string   `json:"title"`
	Widgets *[]Widget `json:"widgets"`
}

func (a *api) updateDashboard(w http.ResponseWriter, r *http.Request) {
	var in dashboardPatch
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Widgets != nil {
		norm := normalizeWidgets(*in.Widgets)
		in.Widgets = &norm
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
	}
	d, ok := a.store.updateDashboard(r.PathValue("id"), in.Title, in.Widgets)
	if !ok {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// addWidget appends one chart widget to a dashboard — the common case when
// the AI assistant assembles a dashboard one chart at a time.
func (a *api) addWidget(w http.ResponseWriter, r *http.Request) {
	var in Widget
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.ChartID) == "" {
		writeErr(w, http.StatusBadRequest, "chart_id is required")
		return
	}
	d, ok := a.store.getDashboard(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	widgets := append(append([]Widget{}, d.Widgets...), normalizeWidget(in))
	updated, _ := a.store.updateDashboard(d.ID, nil, &widgets)
	writeJSON(w, http.StatusOK, updated)
}

func (a *api) deleteDashboard(w http.ResponseWriter, r *http.Request) {
	if !a.store.deleteDashboard(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Reports ───────────────────────────────────────────────────────

type reportInput struct {
	Title  string        `json:"title"`
	Blocks []ReportBlock `json:"blocks"`
}

func (a *api) createReport(w http.ResponseWriter, r *http.Request) {
	var in reportInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	blocks := make([]ReportBlock, 0, len(in.Blocks))
	for _, b := range in.Blocks {
		switch b.Kind {
		case "text":
			blocks = append(blocks, ReportBlock{Kind: "text", Text: b.Text})
		case "chart":
			if strings.TrimSpace(b.ChartID) == "" {
				writeErr(w, http.StatusBadRequest, "a chart block requires a chart_id")
				return
			}
			blocks = append(blocks, ReportBlock{Kind: "chart", ChartID: b.ChartID})
		default:
			writeErr(w, http.StatusBadRequest, "block kind must be 'text' or 'chart'")
			return
		}
	}
	rep := a.store.createReport(&Report{Title: in.Title, Blocks: blocks})
	writeJSON(w, http.StatusCreated, rep)
}

func (a *api) listReports(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.listReports())
}

func (a *api) getReport(w http.ResponseWriter, r *http.Request) {
	rep, ok := a.store.getReport(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "report not found")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (a *api) deleteReport(w http.ResponseWriter, r *http.Request) {
	if !a.store.deleteReport(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "report not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Preview ───────────────────────────────────────────────────────

// preview runs ad-hoc SQL without saving anything. The chart editor uses it
// to show sample rows and offer the result columns as x / y axis choices.
func (a *api) preview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SQL string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.SQL) == "" {
		writeErr(w, http.StatusBadRequest, "sql is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	res := a.ratd.run(ctx, in.SQL)
	writeJSON(w, http.StatusOK, res)
}

// ── Helpers ───────────────────────────────────────────────────────

// normalizeWidget clamps a widget's span to the grid (width 1–4, height 1–3)
// and applies sensible defaults for a zero value.
func normalizeWidget(wgt Widget) Widget {
	if wgt.Width == 0 {
		wgt.Width = 2
	}
	if wgt.Height == 0 {
		wgt.Height = 1
	}
	wgt.Width = clamp(wgt.Width, 1, 4)
	wgt.Height = clamp(wgt.Height, 1, 3)
	return wgt
}

func normalizeWidgets(in []Widget) []Widget {
	out := make([]Widget, 0, len(in))
	for _, wgt := range in {
		if strings.TrimSpace(wgt.ChartID) == "" {
			continue
		}
		out = append(out, normalizeWidget(wgt))
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// cleanStrings trims and drops empty entries from a string slice.
func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
