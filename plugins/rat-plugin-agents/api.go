package main

// api exposes the agent CRUD endpoints. Mounted by ratd at
// /api/v1/x/agents/*.
//
//   GET    /agents          — list all
//   GET    /agents/{id}     — one agent
//   POST   /agents          — create (id auto-assigned if omitted)
//   PUT    /agents/{id}     — update
//   DELETE /agents/{id}     — remove
//   POST   /agents/seed     — populate defaults if catalog is empty
//   GET    /health          — liveness

import (
	"encoding/json"
	"net/http"
	"strings"
)

type api struct {
	store *store
}

func newAPI(s *store) *api { return &api{store: s} }

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /agents", a.list)
	m.HandleFunc("GET /agents/{id}", a.get)
	m.HandleFunc("POST /agents", a.create)
	m.HandleFunc("PUT /agents/{id}", a.update)
	m.HandleFunc("DELETE /agents/{id}", a.delete)
	m.HandleFunc("POST /agents/seed", a.seed)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

func (a *api) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"agents": a.store.list()})
}

func (a *api) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ag, ok := a.store.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, ag)
}

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	var in Agent
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	out, err := a.store.create(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *api) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in Agent
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	out, err := a.store.update(r.Context(), id, in)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.delete(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// seed populates the catalog with defaults — useful from the UI when
// the user wipes everything and wants a fresh starting point. Returns
// the new catalog. If the catalog is non-empty, returns 409.
func (a *api) seed(w http.ResponseWriter, r *http.Request) {
	if len(a.store.list()) > 0 {
		writeErr(w, http.StatusConflict, "catalog is not empty — delete agents first")
		return
	}
	for _, ag := range seedAgents() {
		if _, err := a.store.create(r.Context(), ag); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": a.store.list()})
}
