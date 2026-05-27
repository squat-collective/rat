package main

// HTTP API. Mounted at /api/v1/x/diff/*.

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type api struct {
	store *store
	ib    *icebergDiffer
}

func newAPI(st *store, ib *icebergDiffer) *api {
	return &api{store: st, ib: ib}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /events", a.events)
	m.HandleFunc("GET /tables/{ns}/{layer}/{name}/snapshots", a.snapshots)
	m.HandleFunc("POST /tables/{ns}/{layer}/{name}/diff", a.diff)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

func (a *api) events(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	kindPrefix := r.URL.Query().Get("kind")
	writeJSON(w, http.StatusOK, map[string]any{
		"events": a.store.list(kindPrefix, limit),
	})
}

func (a *api) snapshots(w http.ResponseWriter, r *http.Request) {
	ns, layer, name := r.PathValue("ns"), r.PathValue("layer"), r.PathValue("name")
	snaps, err := a.ib.ListSnapshots(r.Context(), ns, layer, name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"table":     ns + "." + layer + "." + name,
		"snapshots": snaps,
	})
}

type diffSide struct {
	MetadataURL string `json:"metadata_url"`
	// String-encoded — see TableVersion.SnapshotID for the rationale.
	SnapshotID int64 `json:"snapshot_id,string"`
}
type diffRequest struct {
	A     diffSide `json:"a"`
	B     diffSide `json:"b"`
	Limit int      `json:"limit"`
}

func (a *api) diff(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("ns") // path params keep the URL discoverable but the
	_ = r.PathValue("layer") // diff payload is fully self-describing.
	_ = r.PathValue("name")
	var in diffRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.A.MetadataURL == "" || in.B.MetadataURL == "" || in.A.SnapshotID == 0 || in.B.SnapshotID == 0 {
		writeErr(w, http.StatusBadRequest, "a.{metadata_url,snapshot_id} and b.{metadata_url,snapshot_id} are required")
		return
	}
	rd, err := a.ib.Diff(r.Context(),
		in.A.MetadataURL, in.A.SnapshotID,
		in.B.MetadataURL, in.B.SnapshotID,
		in.Limit)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rd)
}
