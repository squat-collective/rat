package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRatd answers the interconnect broker (when brokerOK) and the ai-provider
// direct route — enough to exercise both paths of ratdClient.chat.
func fakeRatd(t *testing.T, brokerOK bool, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"message": map[string]string{"role": "assistant", "content": reply},
			"model":   "m1",
		})
		switch r.URL.Path {
		case "/api/v1/x/interconnect/invoke":
			if !brokerOK {
				http.Error(w, "no interconnect", http.StatusNotFound)
				return
			}
			out, _ := json.Marshal(map[string]any{
				"capability": "ai.chat", "provider": "ai-provider", "status": 200,
				"body": json.RawMessage(body),
			})
			_, _ = w.Write(out)
		case "/api/v1/x/ai-provider/chat":
			_, _ = w.Write(body)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postSuggest(t *testing.T, a *suggestAPI, bodyJSON string) (*httptest.ResponseRecorder, suggestResponse) {
	t.Helper()
	req := httptest.NewRequest("POST", "/suggest", strings.NewReader(bodyJSON))
	rec := httptest.NewRecorder()
	a.handle(rec, req)
	var out suggestResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestSuggestViaBroker(t *testing.T) {
	llmReply := `{"description":"orders","column_descriptions":{"id":"primary key"}}`
	a := &suggestAPI{ratd: newRatdClient(fakeRatd(t, true, llmReply).URL)}

	_, out := postSuggest(t, a, `{"table":{"namespace":"default","layer":"bronze","name":"orders"},"columns":[{"name":"id","type":"BIGINT"}]}`)
	if out.Error != "" {
		t.Fatalf("unexpected error: %s", out.Error)
	}
	if out.Description != "orders" {
		t.Errorf("description = %q", out.Description)
	}
	if out.ColumnDescriptions["id"] != "primary key" {
		t.Errorf("column description missing: %+v", out.ColumnDescriptions)
	}
	if out.Model != "m1" {
		t.Errorf("model = %q", out.Model)
	}
}

func TestSuggestFallsBackToProviderWhenBrokerAbsent(t *testing.T) {
	llmReply := `{"description":"direct","column_descriptions":{"id":"x"}}`
	a := &suggestAPI{ratd: newRatdClient(fakeRatd(t, false, llmReply).URL)}

	_, out := postSuggest(t, a, `{"table":{"namespace":"d","layer":"b","name":"t"},"columns":[{"name":"id"}]}`)
	if out.Error != "" {
		t.Fatalf("unexpected error: %s", out.Error)
	}
	if out.Description != "direct" {
		t.Errorf("description = %q, want 'direct' (fallback path)", out.Description)
	}
}

func TestSuggestRequiresColumns(t *testing.T) {
	a := &suggestAPI{ratd: newRatdClient("http://ratd:8080")}
	rec, _ := postSuggest(t, a, `{"table":{"namespace":"d","layer":"b","name":"t"},"columns":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no columns, got %d", rec.Code)
	}
}

func TestSuggestReportsBadAIJSON(t *testing.T) {
	a := &suggestAPI{ratd: newRatdClient(fakeRatd(t, true, "not json at all").URL)}
	_, out := postSuggest(t, a, `{"table":{"namespace":"d","layer":"b","name":"t"},"columns":[{"name":"x"}]}`)
	if out.Error == "" {
		t.Error("expected an error for non-JSON AI output")
	}
}
