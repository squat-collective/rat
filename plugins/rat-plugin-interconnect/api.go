package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// api serves the interconnect REST API. ratd proxies it at
// /api/v1/x/interconnect/* (wildcard), so every route below is reachable.
type api struct {
	store *store
	ratd  *ratdClient
}

func newAPI(s *store, rc *ratdClient) *api {
	return &api{store: s, ratd: rc}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /mesh", a.getMesh)
	m.HandleFunc("GET /capabilities", a.listCapabilities)
	m.HandleFunc("POST /register", a.register)
	m.HandleFunc("DELETE /capabilities/{name}", a.deleteCapability)
	m.HandleFunc("POST /invoke", a.invoke)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// ── Mesh ──────────────────────────────────────────────────────────

type meshResponse struct {
	Plugins      []pluginInfo  `json:"plugins"`
	Capabilities []*Capability `json:"capabilities"`
	Error        string        `json:"error,omitempty"`
}

// getMesh returns the whole picture: every plugin ratd knows (the nodes) and
// every registered capability (the wiring).
func (a *api) getMesh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	plugins, err := a.ratd.plugins(ctx)
	resp := meshResponse{Plugins: plugins, Capabilities: a.store.list()}
	if err != nil {
		resp.Error = "could not reach ratd: " + err.Error()
	}
	if resp.Plugins == nil {
		resp.Plugins = []pluginInfo{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── Capabilities ──────────────────────────────────────────────────

type registerInput struct {
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Consumers   []string `json:"consumers"`
}

// register adds (or replaces) a capability — a named, brokerable service that
// a plugin offers.
func (a *api) register(w http.ResponseWriter, r *http.Request) {
	var in registerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Provider = strings.TrimSpace(in.Provider)
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	if in.Method == "" {
		in.Method = "POST"
	}
	in.Path = strings.TrimSpace(in.Path)
	if in.Path == "" {
		in.Path = "/"
	}
	if !strings.HasPrefix(in.Path, "/") {
		in.Path = "/" + in.Path
	}
	if in.Name == "" || in.Provider == "" {
		writeErr(w, http.StatusBadRequest, "name and provider are required")
		return
	}
	if !validMethod(in.Method) {
		writeErr(w, http.StatusBadRequest, "method must be GET, POST, PUT or DELETE")
		return
	}
	c := a.store.register(&Capability{
		Name: in.Name, Provider: in.Provider, Method: in.Method,
		Path: in.Path, Description: in.Description, Consumers: cleanStrings(in.Consumers),
	})
	writeJSON(w, http.StatusCreated, c)
}

func (a *api) listCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.list())
}

func (a *api) deleteCapability(w http.ResponseWriter, r *http.Request) {
	if !a.store.delete(r.PathValue("name")) {
		writeErr(w, http.StatusNotFound, "capability not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Invoke (the broker) ───────────────────────────────────────────

type invokeInput struct {
	Capability string          `json:"capability"`
	Payload    json.RawMessage `json:"payload"`
}

type invokeResult struct {
	Capability string          `json:"capability"`
	Provider   string          `json:"provider,omitempty"`
	Status     int             `json:"status,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// invoke is the broker: given a capability name, it finds the providing
// plugin and forwards the call — the caller never names the provider.
func (a *api) invoke(w http.ResponseWriter, r *http.Request) {
	var in invokeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(in.Capability)
	c, ok := a.store.get(name)
	if !ok {
		writeJSON(w, http.StatusOK, invokeResult{
			Capability: name, Error: "no capability registered under that name",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Route only to a healthy provider (best-effort — skipped if ratd is down).
	if plugins, err := a.ratd.plugins(ctx); err == nil && !providerHealthy(plugins, c.Provider) {
		writeJSON(w, http.StatusOK, invokeResult{
			Capability: c.Name, Provider: c.Provider,
			Error: "provider " + c.Provider + " is not registered or not healthy",
		})
		return
	}

	var body []byte
	if c.Method != http.MethodGet && len(in.Payload) > 0 {
		body = in.Payload
	}
	status, out, err := a.ratd.invoke(ctx, c.Provider, c.Method, c.Path, body)
	res := invokeResult{Capability: c.Name, Provider: c.Provider, Status: status}
	if err != nil {
		res.Error = err.Error()
	} else if json.Valid(out) {
		res.Body = out
	} else {
		res.Body, _ = json.Marshal(string(out))
	}
	writeJSON(w, http.StatusOK, res)
}

// ── Helpers ───────────────────────────────────────────────────────

func validMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

func providerHealthy(plugins []pluginInfo, name string) bool {
	for _, p := range plugins {
		if p.Name == name {
			return p.Healthy
		}
	}
	return false
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
