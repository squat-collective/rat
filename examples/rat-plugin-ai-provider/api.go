package main

import (
	"context"
	"encoding/json"
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
func (a *api) chatWithTools(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages []chatMessage     `json:"messages"`
		Tools    []toolDeclaration `json:"tools"`
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
	msg, model, finish, err := a.llm.chatWithTools(ctx, in.Messages, in.Tools)
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
