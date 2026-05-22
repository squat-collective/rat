package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestAPI builds an api wired to a fake ratd that answers the query
// endpoint with one row, so the /query path can be exercised.
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

func TestCreateDashboardRequiresTitle(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/dashboards", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a dashboard with no title, got %d", rec.Code)
	}
}

func TestCreateAndGetDashboard(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/dashboards", `{"title":"Sales"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var d Dashboard
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if d.ID == "" {
		t.Fatal("created dashboard should have an ID")
	}
	rec = do(t, mux, "GET", "/dashboards/"+d.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get expected 200, got %d", rec.Code)
	}
}

func TestAddComponentClampsLayoutAndAssignsID(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/dashboards", `{"title":"Sales"}`)
	var d Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &d)

	rec = do(t, mux, "POST", "/dashboards/"+d.ID+"/components",
		`{"type":"chart","layout":{"x":0,"y":0,"w":99,"h":0},"props":{"title":"X"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("addComponent expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var withC Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &withC)
	if len(withC.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(withC.Components))
	}
	c := withC.Components[0]
	if c.ID == "" {
		t.Error("the component should be assigned an id")
	}
	if c.Layout.W != 12 {
		t.Errorf("width 99 should clamp to 12, got %d", c.Layout.W)
	}
	if c.Layout.H == 0 {
		t.Error("a zero height should get the type default")
	}
}

func TestAddComponentRejectsUnknownType(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/dashboards", `{"title":"Sales"}`)
	var d Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	rec = do(t, mux, "POST", "/dashboards/"+d.ID+"/components", `{"type":"video"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown component type, got %d", rec.Code)
	}
}

func TestUpdateDashboardReplacesComponents(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/dashboards", `{"title":"Sales"}`)
	var d Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &d)

	rec = do(t, mux, "PUT", "/dashboards/"+d.ID,
		`{"components":[{"type":"heading","layout":{"x":0,"y":0,"w":12,"h":2},"props":{"text":"Hello"}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var upd Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &upd)
	if len(upd.Components) != 1 || upd.Components[0].Type != "heading" {
		t.Fatalf("components not replaced: %v", upd.Components)
	}
}

func TestQueryRunsSQL(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/query", `{"sql":"SELECT name, amount FROM t"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res queryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode query result: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 row, got %v", res.Rows)
	}
}
