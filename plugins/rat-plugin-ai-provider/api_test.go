package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestAPI builds an api wired to a fake OpenAI-compatible endpoint.
func newTestAPI(t *testing.T, key string) (*api, http.Handler) {
	t.Helper()
	fakeLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi from the model"}}]}`))
	}))
	t.Cleanup(fakeLLM.Close)

	cfg := newConfigStore("http://ratd:8080", "ai-provider", aiConfig{
		BaseURL: fakeLLM.URL, Model: "test-model", APIKey: key,
	})
	a := newAPI(cfg, newLLM(cfg))
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

func TestCompleteReturnsText(t *testing.T) {
	_, mux := newTestAPI(t, "k")
	rec := do(t, mux, "POST", "/complete", `{"prompt":"hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["error"] != "" {
		t.Fatalf("unexpected error: %s", res["error"])
	}
	if !strings.Contains(res["text"], "hi from the model") {
		t.Errorf("expected the model's text, got %q", res["text"])
	}
	if res["model"] != "test-model" {
		t.Errorf("model = %q, want test-model", res["model"])
	}
}

func TestCompleteRequiresPrompt(t *testing.T) {
	_, mux := newTestAPI(t, "k")
	rec := do(t, mux, "POST", "/complete", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no prompt, got %d", rec.Code)
	}
}

func TestChatReturnsMessage(t *testing.T) {
	_, mux := newTestAPI(t, "k")
	rec := do(t, mux, "POST", "/chat", `{"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res struct {
		Message chatMessage `json:"message"`
		Error   string      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Message.Role != "assistant" || !strings.Contains(res.Message.Content, "hi from the model") {
		t.Errorf("unexpected message: %+v", res.Message)
	}
}

func TestChatRequiresMessages(t *testing.T) {
	_, mux := newTestAPI(t, "k")
	rec := do(t, mux, "POST", "/chat", `{"messages":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no messages, got %d", rec.Code)
	}
}

func TestGetConfigMasksAPIKey(t *testing.T) {
	secret := "super-secret-key-xyz"
	_, mux := newTestAPI(t, secret)
	rec := do(t, mux, "GET", "/config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("/config must not leak the API key")
	}
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["api_key_set"] != true {
		t.Errorf("expected api_key_set true, got %v", res["api_key_set"])
	}
	if res["model"] != "test-model" {
		t.Errorf("model = %v, want test-model", res["model"])
	}
}
