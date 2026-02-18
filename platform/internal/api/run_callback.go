// Package api — internal run status callback endpoint.
// The runner pushes status updates here instead of waiting for ratd to poll.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// MountInternalRoutes registers internal endpoints that are called by other
// services (runner, plugins) rather than by end users. These routes are
// mounted outside the auth middleware — they trust the caller's network identity.
//
// Route: POST /api/v1/internal/runs/{runID}/status
func MountInternalRoutes(r chi.Router, srv *Server) {
	r.Post("/api/v1/internal/runs/{runID}/status", srv.HandleRunStatusCallback)
}

// HandleRunStatusCallback processes a push-based status update from the runner.
// The runner POSTs here when a run reaches a terminal state, eliminating the
// need for frequent polling. Polling at 60s remains as a fallback safety net.
//
// Request body: RunStatusUpdate JSON
// Response: 200 OK on success, 400/404/500 on error
func (s *Server) HandleRunStatusCallback(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")

	var update RunStatusUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Ensure the URL path run ID matches the body (defense in depth)
	if update.RunID != "" && update.RunID != runID {
		errorJSON(w, "run_id in body does not match URL", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	update.RunID = runID

	// Validate status is terminal
	if update.Status != "success" && update.Status != "failed" && update.Status != "cancelled" {
		errorJSON(w, "status must be success, failed, or cancelled", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Delegate to executor if it supports push-based callbacks.
	// Guard against nil executor (e.g., dev mode with no runner configured).
	if s.Executor == nil {
		slog.Warn("status callback received but no executor configured", "run_id", runID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}
	receiver, ok := s.Executor.(StatusCallbackReceiver)
	if !ok {
		// Executor doesn't support callbacks — just accept and ignore.
		// The poll fallback will handle it.
		slog.Warn("status callback received but executor does not support StatusCallbackReceiver",
			"run_id", runID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}

	if err := receiver.HandleStatusCallback(r.Context(), update); err != nil {
		slog.Error("status callback processing failed", "run_id", runID, "error", err)
		internalError(w, "failed to process status callback", err)
		return
	}

	slog.Info("status callback processed", "run_id", runID, "status", update.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}
