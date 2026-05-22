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
func fakeRatd(t *testing.T, brokerOK bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/x/interconnect/invoke":
			if !brokerOK {
				http.Error(w, "no interconnect", http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"capability":"ai.chat","provider":"ai-provider","status":200,` +
				`"body":{"message":{"role":"assistant","content":"brokered reply"},"model":"m1"}}`))
		case "/api/v1/x/ai-provider/chat":
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"direct reply"},"model":"m2"}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postChat(t *testing.T, a *chatAPI, bodyJSON string) (*httptest.ResponseRecorder, map[string]string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(bodyJSON))
	rec := httptest.NewRecorder()
	a.handle(rec, req)
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestChatViaBroker(t *testing.T) {
	a := &chatAPI{ratd: newRatdClient(fakeRatd(t, true).URL)}
	_, out := postChat(t, a, `{"messages":[{"role":"user","content":"hi"}]}`)
	if out["error"] != "" {
		t.Fatalf("unexpected error: %s", out["error"])
	}
	if out["reply"] != "brokered reply" {
		t.Errorf("reply = %q, want 'brokered reply'", out["reply"])
	}
}

func TestChatFallsBackToProviderWhenBrokerAbsent(t *testing.T) {
	a := &chatAPI{ratd: newRatdClient(fakeRatd(t, false).URL)} // interconnect 404s
	_, out := postChat(t, a, `{"messages":[{"role":"user","content":"hi"}]}`)
	if out["error"] != "" {
		t.Fatalf("unexpected error: %s", out["error"])
	}
	if out["reply"] != "direct reply" {
		t.Errorf("reply = %q, want 'direct reply' (the fallback path)", out["reply"])
	}
}

func TestChatRequiresMessages(t *testing.T) {
	a := &chatAPI{ratd: newRatdClient("http://ratd:8080")}
	rec, _ := postChat(t, a, `{"messages":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no messages, got %d", rec.Code)
	}
}
