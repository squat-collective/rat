package main

// HTTP API for the secrets plugin. Mounted at /api/v1/x/secrets/* by
// ratd's plugin proxy. Two surfaces:
//
//   - Management (admin/UI): list / upsert / delete. Returns names +
//     metadata only — never the plaintext value.
//   - Resolution (other plugins via the interconnect broker):
//     POST /resolve { name } → { name, value }. Single endpoint so the
//     broker can forward straight through with no path-templating.
//
// All endpoints are reachable from anywhere on the same docker network
// (including the browser via ratd's proxy). For single-user demo trust
// this is fine; a real deployment would gate /resolve on a token.

import (
	"encoding/json"
	"errors"
	"net/http"
)

type api struct {
	store *store
}

func newAPI(s *store) *api { return &api{store: s} }

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /secrets", a.list)
	m.HandleFunc("POST /secrets", a.upsert)
	m.HandleFunc("DELETE /secrets/{name}", a.delete)
	m.HandleFunc("POST /resolve", a.resolve)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

func (a *api) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"secrets": a.store.list()})
}

func (a *api) upsert(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sec, err := a.store.upsert(r.Context(), in.Name, in.Value, in.Description)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, SecretSummary{
		Name: sec.Name, Description: sec.Description,
		CreatedAt: sec.CreatedAt, UpdatedAt: sec.UpdatedAt,
	})
}

func (a *api) delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.store.delete(r.Context(), name); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "secret not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolve is the consumer-plugin path — used via the interconnect
// broker as the `secrets.get` capability.
func (a *api) resolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	value, err := a.store.get(in.Name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "secret not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"name":  in.Name,
		"value": value,
	})
}
