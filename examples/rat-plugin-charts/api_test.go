package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestAPI builds an api wired to a fake ratd that always answers the query
// endpoint with one row, so chart-data and preview paths can be exercised.
func newTestAPI(t *testing.T) (*api, http.Handler) {
	t.Helper()
	fakeRatd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":[{"name":"Alice","amount":10}],"total_rows":1}`))
	}))
	t.Cleanup(fakeRatd.Close)

	a := newAPI(newStore(), newRatdClient(fakeRatd.URL))
	return a, a.mux()
}

func do(t *testing.T, mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCreateChartRequiresFields(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/charts", `{"title":"No SQL"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a chart missing sql/columns, got %d", rec.Code)
	}
}

func TestCreateChartRejectsBadType(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/charts",
		`{"title":"X","type":"pizza","sql":"SELECT 1","x_column":"a","y_columns":["b"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown chart type, got %d", rec.Code)
	}
}

func TestCreateChartStoresAndClampsOptions(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/charts",
		`{"title":"Styled","type":"radar","sql":"SELECT a,b FROM t","x_column":"a","y_columns":["b"],`+
			`"options":{"palette":"ocean","stacked":true,"curve":"step","bar_radius":99,"inner_radius":-5}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 (radar is a valid type), got %d (%s)", rec.Code, rec.Body.String())
	}
	var c Chart
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode chart: %v", err)
	}
	if c.Options.Palette != "ocean" || !c.Options.Stacked || c.Options.Curve != "step" {
		t.Errorf("options not stored: %+v", c.Options)
	}
	if c.Options.BarRadius != 16 {
		t.Errorf("bar_radius should clamp to 16, got %d", c.Options.BarRadius)
	}
	if c.Options.InnerRadius != 0 {
		t.Errorf("inner_radius should clamp to 0, got %d", c.Options.InnerRadius)
	}
}

func TestCreateAndListCharts(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/charts",
		`{"title":"Sales","type":"bar","sql":"SELECT name, amount FROM t","x_column":"name","y_columns":["amount"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created Chart
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created chart: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created chart should have an ID")
	}

	rec = do(t, mux, "GET", "/charts", "")
	var list []Chart
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode chart list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("expected the created chart in the list, got %v", list)
	}
}

func TestGetChartDataRunsQuery(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/charts",
		`{"title":"Sales","type":"bar","sql":"SELECT name, amount FROM t","x_column":"name","y_columns":["amount"]}`)
	var created Chart
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = do(t, mux, "GET", "/charts/"+created.ID+"/data", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var data chartData
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode chart data: %v", err)
	}
	if data.Error != "" {
		t.Fatalf("unexpected error: %s", data.Error)
	}
	if len(data.Rows) != 1 || data.Rows[0]["name"] != "Alice" {
		t.Fatalf("expected the fake ratd row, got %v", data.Rows)
	}
}

func TestGetChartDataNotFound(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "GET", "/charts/does-not-exist/data", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a missing chart, got %d", rec.Code)
	}
}

func TestDashboardLifecycle(t *testing.T) {
	_, mux := newTestAPI(t)

	rec := do(t, mux, "POST", "/dashboards", `{"title":"Ops"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	var dash Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &dash)

	// Add a widget — width 9 must be clamped to the 1–4 grid.
	rec = do(t, mux, "POST", "/dashboards/"+dash.ID+"/widgets",
		`{"chart_id":"chart-1","width":9,"height":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("addWidget expected 200, got %d", rec.Code)
	}
	var withWidget Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &withWidget)
	if len(withWidget.Widgets) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(withWidget.Widgets))
	}
	if withWidget.Widgets[0].Width != 4 {
		t.Errorf("widget width should clamp to 4, got %d", withWidget.Widgets[0].Width)
	}

	rec = do(t, mux, "DELETE", "/dashboards/"+dash.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete expected 204, got %d", rec.Code)
	}
	rec = do(t, mux, "GET", "/dashboards/"+dash.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("dashboard should be gone, got %d", rec.Code)
	}
}

func TestCreateReportRejectsBadBlockKind(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/reports",
		`{"title":"R","blocks":[{"kind":"video"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown block kind, got %d", rec.Code)
	}
}

func TestCreateReportChartBlockNeedsChartID(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/reports",
		`{"title":"R","blocks":[{"kind":"chart"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a chart block with no chart_id, got %d", rec.Code)
	}
}

func TestPreviewRunsQuery(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/preview", `{"sql":"SELECT name, amount FROM t"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res queryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 preview row, got %v", res.Rows)
	}
}

func TestNormalizeWidgetClamps(t *testing.T) {
	got := normalizeWidget(Widget{ChartID: "c"})
	if got.Width != 2 || got.Height != 1 {
		t.Errorf("zero widget should default to 2x1, got %dx%d", got.Width, got.Height)
	}
	got = normalizeWidget(Widget{ChartID: "c", Width: 99, Height: 99})
	if got.Width != 4 || got.Height != 3 {
		t.Errorf("oversized widget should clamp to 4x3, got %dx%d", got.Width, got.Height)
	}
}
