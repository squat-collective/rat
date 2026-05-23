package main

import (
	"embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// api serves the demo-loader REST endpoints. ratd proxies them at
// /api/v1/x/demo-loader/*.
type api struct {
	installer *Installer
	files     embed.FS
}

func newAPI(installer *Installer, files embed.FS) *api {
	return &api{installer: installer, files: files}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /demos", a.listDemos)
	m.HandleFunc("POST /install", a.install)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// demoSummary is the lightweight view shown in the L3 list.
type demoSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Namespace     string `json:"namespace"`
	PipelineCount int    `json:"pipeline_count"`
	TestCount     int    `json:"test_count"`
}

func (a *api) loadManifests() ([]Manifest, error) {
	entries, err := a.files.ReadDir("demos")
	if err != nil {
		return nil, err
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := a.files.ReadFile("demos/" + e.Name() + "/manifest.json")
		if err != nil {
			continue
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if m.ID == "" {
			m.ID = e.Name()
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (a *api) listDemos(w http.ResponseWriter, _ *http.Request) {
	ms, err := a.loadManifests()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read demos: "+err.Error())
		return
	}
	out := make([]demoSummary, 0, len(ms))
	for _, m := range ms {
		out = append(out, demoSummary{
			ID: m.ID, Name: m.Name, Description: m.Description,
			Namespace: m.Namespace,
			PipelineCount: len(m.Pipelines),
			TestCount:     len(m.Tests),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type installRequest struct {
	DemoID    string `json:"demo_id"`
	Namespace string `json:"namespace"`
}

func (a *api) install(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.DemoID) == "" {
		writeErr(w, http.StatusBadRequest, "demo_id is required")
		return
	}

	ms, err := a.loadManifests()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read demos: "+err.Error())
		return
	}
	var target *Manifest
	for i := range ms {
		if ms[i].ID == req.DemoID {
			target = &ms[i]
			break
		}
	}
	if target == nil {
		writeErr(w, http.StatusNotFound, "no such demo: "+req.DemoID)
		return
	}

	res := a.installer.Install(r.Context(), target, req.Namespace)
	writeJSON(w, http.StatusOK, res)
}
