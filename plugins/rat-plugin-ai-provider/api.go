package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// api serves the AI provider's REST API. ratd proxies it at
// /api/v1/x/ai-provider/* — /complete and /chat are the primitives other
// plugins reuse.
type api struct {
	cfg *configStore
	llm *llm
}

func newAPI(cfg *configStore, l *llm) *api {
	return &api{cfg: cfg, llm: l}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("POST /complete", a.complete)
	m.HandleFunc("POST /chat", a.chat)
	m.HandleFunc("POST /chat-with-tools", a.chatWithTools)
	m.HandleFunc("POST /chat-with-tools-stream", a.chatWithToolsStream)
	m.HandleFunc("GET /config", a.getConfig)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// complete is the one-shot completion primitive: a prompt in, text out.
func (a *api) complete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt string `json:"prompt"`
		System string `json:"system"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, "prompt is required")
		return
	}
	system := strings.TrimSpace(in.System)
	if system == "" {
		system = a.cfg.get().SystemPrompt
	}

	var msgs []chatMessage
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: system})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: in.Prompt})

	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	msg, model, err := a.llm.chat(ctx, msgs)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": msg.Content, "model": model})
}

// chat is the raw multi-message chat primitive — for callers that manage their
// own conversation.
func (a *api) chat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages []chatMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(in.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	msg, model, err := a.llm.chat(ctx, in.Messages)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": msg, "model": model})
}

// chatWithTools is the tool-aware chat primitive: the caller passes an
// array of tool declarations and gets back the assistant's tool_calls +
// finish_reason. The caller (e.g. the rat-plugin-chat orchestrator) is
// responsible for executing the tools and feeding results back in the
// next call. We deliberately do NOT loop here — the multi-turn dance is
// the chat plugin's job, not ours.
//
// Optional `model` and `temperature` fields let the caller (typically an
// agent) override the plugin's configured defaults per call.
func (a *api) chatWithTools(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages    []chatMessage     `json:"messages"`
		Tools       []toolDeclaration `json:"tools"`
		Model       string            `json:"model,omitempty"`
		Temperature float64           `json:"temperature,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(in.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	msg, model, finish, err := a.llm.chatWithTools(ctx, in.Messages, in.Tools, callOverrides{Model: in.Model, Temperature: in.Temperature})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message":       msg,
		"model":         model,
		"finish_reason": finish,
	})
}

// chatWithToolsStream is the streaming version of chat-with-tools. The
// response is a Server-Sent Events stream; events emitted:
//
//   event: delta            — one raw upstream delta {content?, tool_calls?, role?}
//   event: done             — { message: <assembled>, model, finish_reason }
//   event: error            — { error: <string> }
//
// The caller (e.g. chat orchestrator) re-emits these to its own SSE stream
// so the UI can paint tokens as they arrive and still get the fully
// assembled message at the end for the next loop iteration.
func (a *api) chatWithToolsStream(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages    []chatMessage     `json:"messages"`
		Tools       []toolDeclaration `json:"tools"`
		Model       string            `json:"model,omitempty"`
		Temperature float64           `json:"temperature,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(in.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages is required")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sink := &sseEmitter{w: w, flusher: flusher}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	msg, model, finish, err := a.llm.chatWithToolsStream(ctx, in.Messages, in.Tools, callOverrides{Model: in.Model, Temperature: in.Temperature}, sink)
	if err != nil {
		_ = sink.emit("error", map[string]string{"error": err.Error()})
		return
	}
	_ = sink.emit("done", map[string]any{
		"message":       msg,
		"model":         model,
		"finish_reason": finish,
	})
}

// sseEmitter writes one SSE event per call. Used by the streaming endpoint
// and by the chatWithToolsStream LLM call to forward upstream deltas.
type sseEmitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (e *sseEmitter) emit(event string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
		return err
	}
	e.flusher.Flush()
	return nil
}

// getConfig returns the current EFFECTIVE config, with the API key masked —
// for the status page and for debugging.
func (a *api) getConfig(w http.ResponseWriter, _ *http.Request) {
	c := a.cfg.get()
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url":      c.BaseURL,
		"model":         c.Model,
		"api_key_set":   strings.TrimSpace(c.APIKey) != "",
		"system_prompt": c.SystemPrompt,
	})
}
