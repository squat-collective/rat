package main

// HTTP API mounted at /api/v1/x/pg-sync/*. Two resources:
//
//   - connections: name + secret pointer (the secret holds the postgresql:// URL)
//   - tables:      one row per table mirrored from external pg → Iceberg
//
// Every mutating endpoint that touches a table sync calls engine.Apply
// (or Teardown) so ratd's pipeline catalog stays in lockstep with our
// own state.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type api struct {
	store   *store
	cfg     *configStore
	engine  *syncEngine
	secrets *secretsClient
}

func newAPI(st *store, cfg *configStore, eng *syncEngine, sc *secretsClient) *api {
	return &api{store: st, cfg: cfg, engine: eng, secrets: sc}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /connections", a.listConnections)
	m.HandleFunc("POST /connections", a.upsertConnection)
	m.HandleFunc("DELETE /connections/{name}", a.deleteConnection)
	m.HandleFunc("POST /connections/{name}/test", a.testConnection)

	m.HandleFunc("GET /tables", a.listTables)
	m.HandleFunc("POST /tables", a.createTable)
	m.HandleFunc("PUT /tables/{id}", a.updateTable)
	m.HandleFunc("DELETE /tables/{id}", a.deleteTable)
	m.HandleFunc("POST /tables/{id}/sync-now", a.syncNow)

	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// ── Connections ────────────────────────────────────────────────────

func (a *api) listConnections(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"connections": a.store.listConnections()})
}

func (a *api) upsertConnection(w http.ResponseWriter, r *http.Request) {
	var in Connection
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.SecretName == "" {
		writeErr(w, http.StatusBadRequest, "secret_name is required")
		return
	}
	saved, snap := a.store.upsertConnection(in)
	if err := a.cfg.persist(r.Context(), snap); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (a *api) deleteConnection(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ok, snap, orphaned := a.store.deleteConnection(name)
	if !ok {
		writeErr(w, http.StatusNotFound, "connection not found")
		return
	}
	if err := a.cfg.persist(r.Context(), snap); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Best-effort teardown of orphaned table pipelines + schedules; we
	// don't fail the API call since the state-of-record (our store) is
	// already updated.
	for i := range orphaned {
		_ = a.engine.Teardown(r.Context(), &orphaned[i])
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) testConnection(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	conn, ok := a.store.getConnection(name)
	if !ok {
		writeErr(w, http.StatusNotFound, "connection not found")
		return
	}
	// "Test" right now = can we resolve the secret. A live TCP probe
	// would require pg_isready or a duckdb shell — out of scope for the
	// plugin runtime. The actual TCP/auth check happens on the next run.
	if _, err := a.secrets.Resolve(r.Context(), conn.SecretName); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "stage": "secret",
			"detail": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "stage": "secret",
		"detail": "secret resolved; run the pipeline to verify TCP + auth",
	})
}

// ── Tables ─────────────────────────────────────────────────────────

func (a *api) listTables(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tables": a.store.listTables()})
}

type tableInput struct {
	Connection      string   `json:"connection"`
	SourceSchema    string   `json:"source_schema"`
	SourceTable     string   `json:"source_table"`
	TargetNamespace string   `json:"target_namespace"`
	TargetLayer     string   `json:"target_layer"`
	TargetName      string   `json:"target_name"`
	Mode            SyncMode `json:"mode"`
	WatermarkColumn string   `json:"watermark_column"`
	PrimaryKey      string   `json:"primary_key"`
	Cron            string   `json:"cron"`
	Enabled         *bool    `json:"enabled"`
}

func (a *api) createTable(w http.ResponseWriter, r *http.Request) {
	var in tableInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	t := TableSync{
		ID:              uuid.NewString(),
		Connection:      in.Connection,
		SourceSchema:    in.SourceSchema,
		SourceTable:     in.SourceTable,
		TargetNamespace: in.TargetNamespace,
		TargetLayer:     in.TargetLayer,
		TargetName:      in.TargetName,
		Mode:            in.Mode,
		WatermarkColumn: in.WatermarkColumn,
		PrimaryKey:      in.PrimaryKey,
		Cron:            in.Cron,
		Enabled:         true,
	}
	if in.Enabled != nil {
		t.Enabled = *in.Enabled
	}
	if err := t.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := a.store.getConnection(t.Connection); !ok {
		writeErr(w, http.StatusBadRequest, "connection "+t.Connection+" does not exist")
		return
	}

	saved, snap := a.store.upsertTable(t)
	if err := a.cfg.persist(r.Context(), snap); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := a.engine.Apply(r.Context(), saved); err != nil {
		// Mark the table as broken but keep it in state so the user can
		// see + fix the error in the UI rather than silently losing it.
		_, _ = a.store.markSynced(saved.ID, time.Now().UTC(), "apply failed: "+err.Error())
		_ = a.cfg.persist(context.Background(), a.store.snapshot())
		writeErr(w, http.StatusBadGateway, "saved but pipeline generation failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (a *api) updateTable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := a.store.getTable(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "table not found")
		return
	}
	var in tableInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Patch-style update: fields the caller didn't supply keep their
	// existing values. UI does PUT-with-full-doc, but this keeps the
	// API permissive for raw curl use.
	patched := *existing
	if in.Connection != "" {
		patched.Connection = in.Connection
	}
	if in.SourceSchema != "" {
		patched.SourceSchema = in.SourceSchema
	}
	if in.SourceTable != "" {
		patched.SourceTable = in.SourceTable
	}
	if in.TargetNamespace != "" {
		patched.TargetNamespace = in.TargetNamespace
	}
	if in.TargetLayer != "" {
		patched.TargetLayer = in.TargetLayer
	}
	if in.TargetName != "" {
		patched.TargetName = in.TargetName
	}
	if in.Mode != "" {
		patched.Mode = in.Mode
	}
	if in.WatermarkColumn != "" {
		patched.WatermarkColumn = in.WatermarkColumn
	}
	if in.PrimaryKey != "" {
		patched.PrimaryKey = in.PrimaryKey
	}
	if in.Cron != "" {
		patched.Cron = in.Cron
	}
	if in.Enabled != nil {
		patched.Enabled = *in.Enabled
	}
	if err := patched.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	saved, snap := a.store.upsertTable(patched)
	if err := a.cfg.persist(r.Context(), snap); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.engine.Apply(r.Context(), saved); err != nil {
		_, _ = a.store.markSynced(saved.ID, time.Now().UTC(), "apply failed: "+err.Error())
		_ = a.cfg.persist(context.Background(), a.store.snapshot())
		writeErr(w, http.StatusBadGateway, "saved but pipeline regeneration failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (a *api) deleteTable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleted, snap, ok := a.store.deleteTable(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "table not found")
		return
	}
	if err := a.cfg.persist(r.Context(), snap); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.engine.Teardown(r.Context(), deleted); err != nil {
		// Don't roll the deletion back — the state-of-record is gone.
		// The user can manually clean up the orphaned pipeline if needed.
		writeJSON(w, http.StatusAccepted, map[string]string{"teardown_error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) syncNow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, ok := a.store.getTable(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "table not found")
		return
	}
	runID, err := a.engine.SyncNow(r.Context(), t)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeErr(w, http.StatusGatewayTimeout, err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": runID})
}
