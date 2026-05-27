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

	// Bind run_id (+ request_id when present) to the scope for every log
	// statement below. The chi RequestID middleware already populated the
	// request context with whatever X-Request-ID the runner echoed back, so
	// pulling it explicitly here pairs request_id with run_id in JSON output.
	log := slog.With("run_id", runID)
	if reqID := RequestIDFromContext(r.Context()); reqID != "" {
		log = log.With("request_id", reqID)
	}

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
		log.Warn("status callback received but no executor configured")
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}
	receiver, ok := s.Executor.(StatusCallbackReceiver)
	if !ok {
		// Executor doesn't support callbacks — just accept and ignore.
		// The poll fallback will handle it.
		log.Warn("status callback received but executor does not support StatusCallbackReceiver")
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}

	if err := receiver.HandleStatusCallback(r.Context(), update); err != nil {
		log.Error("status callback processing failed", "error", err)
		internalError(w, "failed to process status callback", err)
		return
	}

	log.Info("status callback processed", "status", update.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}
