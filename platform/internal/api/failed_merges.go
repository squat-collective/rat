// Package api — internal audit endpoint for Phase 5 (branch merge) failures.
//
// The runner posts here when a pipeline run reaches Phase 5 (merge ephemeral
// branch into main) and exhausts its retry budget. We persist the audit row
// so an operator can recover the retained branch by hand, and so the reaper
// can avoid sweeping branches that are still pending recovery.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// FailedMergesStore persists Phase 5 merge failure audit records.
type FailedMergesStore interface {
	Create(ctx context.Context, fm domain.FailedMerge) error
	RecentBranchNames(ctx context.Context, since time.Time) ([]string, error)
}

// MountInternalFailedMergesRoute registers the POST endpoint on the internal
// listener so the runner can record terminal merge failures. This lives next
// to the run-status callback because both are trusted service-to-service
// calls — they share the no-auth trust boundary of the internal listener.
//
// Route: POST /api/v1/internal/failed-merges
func MountInternalFailedMergesRoute(r chi.Router, srv *Server) {
	r.Post("/api/v1/internal/failed-merges", srv.HandleRecordFailedMerge)
}

// HandleRecordFailedMerge accepts a JSON FailedMerge from the runner and
// persists it via the FailedMergesStore. The request is intentionally
// tolerant of fields the caller may not be able to populate (e.g.
// source/target hash when Nessie was unreachable), but run_id, branch_name,
// error_kind, and error_message are required.
func (s *Server) HandleRecordFailedMerge(w http.ResponseWriter, r *http.Request) {
	log := slog.Default()
	if reqID := RequestIDFromContext(r.Context()); reqID != "" {
		log = log.With("request_id", reqID)
	}

	var fm domain.FailedMerge
	if err := json.NewDecoder(r.Body).Decode(&fm); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if fm.RunID == "" || fm.BranchName == "" || fm.ErrorKind == "" || fm.ErrorMessage == "" {
		errorJSON(w, "run_id, branch_name, error_kind and error_message are required",
			"INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Persist even when FailedMerges is unwired (dev mode w/o DB) so the
	// runner does not loop on a real network error vs a missing config.
	if s.FailedMerges == nil {
		log.Warn("failed-merge callback received but FailedMerges store not configured",
			"run_id", fm.RunID, "branch", fm.BranchName)
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}

	if err := s.FailedMerges.Create(r.Context(), fm); err != nil {
		log.Error("failed-merge audit insert failed",
			"run_id", fm.RunID, "branch", fm.BranchName, "error", err)
		internalError(w, "failed to record failed merge", err)
		return
	}

	log.Error("Phase 5 merge failed — branch retained for recovery",
		"run_id", fm.RunID,
		"branch", fm.BranchName,
		"error_kind", fm.ErrorKind,
		"error_message", fm.ErrorMessage,
		"merge_lost_data", true,
	)
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}
