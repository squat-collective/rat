package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestAPI builds an api wired to a fake ratd that reports two plugins and
// answers a demo provider route — enough to exercise /mesh and /invoke.
func newTestAPI(t *testing.T) (*api, http.Handler) {
	t.Helper()
	fakeRatd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/plugins":
			_, _ = w.Write([]byte(`[{"name":"demo","healthy":true,"kind":"platform"},` +
				`{"name":"interconnect","healthy":true,"kind":"platform"}]`))
		case "/api/v1/x/demo/ping":
			_, _ = w.Write([]byte(`{"pong":true}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
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

func TestRegisterRequiresFields(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/register", `{"name":"x"}`) // no provider
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when provider is missing, got %d", rec.Code)
	}
}

func TestRegisterRejectsBadMethod(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/register", `{"name":"x","provider":"demo","method":"FETCH"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unsupported method, got %d", rec.Code)
	}
}

func TestRegisterAndList(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/register",
		`{"name":"demo.ping","provider":"demo","method":"GET","path":"/ping","description":"a ping"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = do(t, mux, "GET", "/capabilities", "")
	var caps []Capability
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if len(caps) != 1 || caps[0].Name != "demo.ping" {
		t.Fatalf("expected the registered capability, got %v", caps)
	}
}

func TestGetMesh(t *testing.T) {
	_, mux := newTestAPI(t)
	do(t, mux, "POST", "/register", `{"name":"demo.ping","provider":"demo","method":"GET","path":"/ping"}`)
	rec := do(t, mux, "GET", "/mesh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var m meshResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode mesh: %v", err)
	}
	if len(m.Plugins) != 2 {
		t.Errorf("expected 2 plugins from ratd, got %d", len(m.Plugins))
	}
	if len(m.Capabilities) != 1 {
		t.Errorf("expected 1 capability, got %d", len(m.Capabilities))
	}
}

func TestInvokeUnknownCapability(t *testing.T) {
	_, mux := newTestAPI(t)
	rec := do(t, mux, "POST", "/invoke", `{"capability":"nope"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res invokeResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Error == "" {
		t.Error("expected an error for an unregistered capability")
	}
}

func TestInvokeBrokersToProvider(t *testing.T) {
	_, mux := newTestAPI(t)
	do(t, mux, "POST", "/register", `{"name":"demo.ping","provider":"demo","method":"GET","path":"/ping"}`)
	rec := do(t, mux, "POST", "/invoke", `{"capability":"demo.ping"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res invokeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode invoke result: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Provider != "demo" {
		t.Errorf("provider = %q, want demo", res.Provider)
	}
	if !strings.Contains(string(res.Body), "pong") {
		t.Errorf("expected the provider's response in the body, got %s", res.Body)
	}
}

func TestDeleteCapability(t *testing.T) {
	_, mux := newTestAPI(t)
	do(t, mux, "POST", "/register", `{"name":"x","provider":"demo"}`)
	rec := do(t, mux, "DELETE", "/capabilities/x", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	rec = do(t, mux, "DELETE", "/capabilities/x", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("deleting a missing capability should 404, got %d", rec.Code)
	}
}
