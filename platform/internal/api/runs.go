package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// LogEntry represents a single log line from a pipeline run.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// RunStore defines the persistence interface for pipeline runs.
// Implemented by postgres store (production) and memory store (tests).
type RunStore interface {
	ListRuns(ctx context.Context, filter RunFilter) ([]domain.Run, error)
	CountRuns(ctx context.Context, filter RunFilter) (int, error)
	GetRun(ctx context.Context, runID string) (*domain.Run, error)
	CreateRun(ctx context.Context, run *domain.Run) error
	UpdateRunStatus(ctx context.Context, runID string, status domain.RunStatus, errMsg *string, durationMs *int64, rowsWritten *int64) error
	GetRunLogs(ctx context.Context, runID string) ([]LogEntry, error)
	SaveRunLogs(ctx context.Context, runID string, logs []LogEntry) error
	DeleteRunsBeyondLimit(ctx context.Context, pipelineID uuid.UUID, keepCount int) (int, error)
	DeleteRunsOlderThan(ctx context.Context, olderThan time.Time) (int, error)
	ListStuckRuns(ctx context.Context, olderThan time.Time) ([]domain.Run, error)

	// LatestRunPerPipeline returns the most recent run for each of the given pipeline IDs
	// in a single batch query, avoiding N+1 queries when building the lineage graph.
	// The returned map is keyed by pipeline ID.
	LatestRunPerPipeline(ctx context.Context, pipelineIDs []uuid.UUID) (map[uuid.UUID]*domain.Run, error)
}

// Allowed sort fields for run list endpoints.
var runSortFields = map[string]bool{
	"created_at":  true,
	"started_at":  true,
	"finished_at": true,
	"status":      true,
	"trigger":     true,
	"duration_ms": true,
}

// RunFilter holds optional filters for listing runs.
// Limit and Offset enable SQL-level pagination. Zero Limit means no limit (return all).
type RunFilter struct {
	Namespace  string
	Layer      string
	Pipeline   string
	Status     string
	PipelineID string // filter by pipeline UUID (used by scheduler to check active runs)
	StartedAfter  *time.Time // filter runs started after this time (P10-101)
	StartedBefore *time.Time // filter runs started before this time (P10-101)
	Limit      int
	Offset     int
	Sort       *SortOrder // optional sort directive (P10-100)
}

// CreateRunRequest is the JSON body for POST /api/v1/runs.
type CreateRunRequest struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Pipeline  string `json:"pipeline"`
	Trigger   string `json:"trigger"`
}

// MountRunRoutes registers run endpoints on the router.
func MountRunRoutes(r chi.Router, srv *Server) {
	r.Get("/runs", srv.HandleListRuns)
	r.Post("/runs", srv.HandleCreateRun)
	r.Get("/runs/{runID}", srv.HandleGetRun)
	r.Post("/runs/{runID}/cancel", srv.HandleCancelRun)
	r.Get("/runs/{runID}/logs", srv.HandleGetRunLogs)
}

// HandleListRuns returns runs, optionally filtered by pipeline, status, and date range.
// Pagination is pushed to SQL via LIMIT/OFFSET for efficiency.
// Date range filters: ?started_after=RFC3339 and ?started_before=RFC3339.
// Sorting: ?sort=field or ?sort=-field (descending).
func (s *Server) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	filter := RunFilter{
		Namespace: r.URL.Query().Get("namespace"),
		Layer:     r.URL.Query().Get("layer"),
		Pipeline:  r.URL.Query().Get("pipeline"),
		Status:    r.URL.Query().Get("status"),
		Limit:     limit,
		Offset:    offset,
		Sort:      parseSorting(r, runSortFields),
	}

	// Parse optional date range filters.
	if v := r.URL.Query().Get("started_after"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.StartedAfter = &t
		} else {
			errorJSON(w, "started_after must be RFC3339 format", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
	}
	if v := r.URL.Query().Get("started_before"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.StartedBefore = &t
		} else {
			errorJSON(w, "started_before must be RFC3339 format", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
	}

	runs, err := s.Runs.ListRuns(r.Context(), filter)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	total, err := s.Runs.CountRuns(r.Context(), filter)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":  runs,
		"total": total,
	})
}

// HandleGetRun returns a single run by ID.
func (s *Server) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")

	run, err := s.Runs.GetRun(r.Context(), runID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if run == nil {
		errorJSON(w, "run not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, run)
}

// HandleCreateRun triggers a new pipeline run.
// For now, creates a record with "pending" status. Actual execution comes
// when the runner gRPC service is wired.
func (s *Server) HandleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req CreateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.Namespace == "" || req.Layer == "" || req.Pipeline == "" {
		errorJSON(w, "namespace, layer, and pipeline are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(req.Namespace) || !validName(req.Pipeline) {
		errorJSON(w, "namespace and pipeline must be a lowercase slug (a-z, 0-9, hyphens, underscores; must start with a letter)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !domain.ValidLayer(req.Layer) {
		errorJSON(w, "layer must be bronze, silver, or gold", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Trigger == "" {
		req.Trigger = "manual"
	}

	// Verify pipeline exists
	pipeline, err := s.Pipelines.GetPipeline(r.Context(), req.Namespace, req.Layer, req.Pipeline)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Triggering a run = write access on the pipeline.
	if !s.requireAccess(w, r, "pipeline", pipeline.ID.String(), "write") {
		return
	}

	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    req.Trigger,
	}

	if err := s.Runs.CreateRun(r.Context(), run); err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Inject cloud credentials if cloud plugin is available
	if s.Cloud != nil && s.Cloud.CloudEnabled() {
		user := plugins.UserFromContext(r.Context())
		if user != nil {
			creds, err := s.Cloud.GetCredentials(r.Context(), user.UserId, req.Namespace)
			if err != nil {
				slog.Warn("cloud credentials unavailable, using defaults", "error", err)
			} else {
				run.S3Overrides = map[string]string{
					"ACCESS_KEY":     creds.AccessKey,
					"SECRET_KEY":     creds.SecretKey,
					"SESSION_TOKEN":  creds.SessionToken,
					"ENDPOINT":       creds.Endpoint,
					"BUCKET":         creds.Bucket,
				}
			}
		}
	}

	// Dispatch to executor if available
	if s.Executor != nil {
		if err := s.Executor.Submit(r.Context(), run, pipeline); err != nil {
			slog.Error("executor submit failed", "run_id", run.ID, "error", err)
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"run_id": run.ID.String(),
		"status": run.Status,
	})
}

// HandleCancelRun cancels a running pipeline.
func (s *Server) HandleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")

	run, err := s.Runs.GetRun(r.Context(), runID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if run == nil {
		errorJSON(w, "run not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Can only cancel pending or running
	if run.Status != domain.RunStatusPending && run.Status != domain.RunStatusRunning {
		errorJSON(w, "run is not cancellable (status: "+string(run.Status)+")", "ALREADY_EXISTS", http.StatusConflict)
		return
	}

	if err := s.Runs.UpdateRunStatus(r.Context(), runID, domain.RunStatusCancelled, nil, nil, nil); err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Best-effort cancel in executor
	if s.Executor != nil {
		_ = s.Executor.Cancel(r.Context(), runID)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"run_id": runID,
		"status": string(domain.RunStatusCancelled),
	})
}

// isTerminalStatus returns true if the run status is a final state.
func isTerminalStatus(s domain.RunStatus) bool {
	return s == domain.RunStatusSuccess || s == domain.RunStatusFailed || s == domain.RunStatusCancelled
}

// HandleGetRunLogs streams run logs as Server-Sent Events.
// For active runs, keeps the connection open and polls for new logs.
// Falls back to JSON array if Accept header doesn't request SSE.
func (s *Server) HandleGetRunLogs(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")

	run, err := s.Runs.GetRun(r.Context(), runID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if run == nil {
		errorJSON(w, "run not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Check if client wants SSE
	if r.Header.Get("Accept") == "text/event-stream" {
		// Enforce SSE connection limits to prevent DoS.
		ip := clientIP(r)
		if s.SSELimiter != nil && !s.SSELimiter.Acquire(ip) {
			errorJSON(w, "too many SSE connections", "RESOURCE_EXHAUSTED", http.StatusTooManyRequests)
			return
		}
		s.streamRunLogs(w, r, runID, run, ip)
		return
	}

	// JSON fallback — try executor first for active runs
	var logs []LogEntry
	if s.Executor != nil && !isTerminalStatus(run.Status) {
		executorLogs, err := s.Executor.GetLogs(r.Context(), runID)
		if err == nil {
			logs = executorLogs
		}
	}
	if logs == nil {
		dbLogs, err := s.Runs.GetRunLogs(r.Context(), runID)
		if err != nil {
			internalError(w, "internal error", err)
			return
		}
		logs = dbLogs
	}
	if logs == nil {
		logs = []LogEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   logs,
		"status": run.Status,
	})
}

// streamRunLogs implements the SSE streaming path for run logs.
// It keeps the connection open, polls for new logs every 2 seconds,
// and closes when the run reaches a terminal state or the max duration is reached.
// The ip parameter is used to release the SSE limiter slot on exit.
func (s *Server) streamRunLogs(w http.ResponseWriter, r *http.Request, runID string, run *domain.Run, ip string) {
	// Release SSE limiter slot when the connection ends.
	if s.SSELimiter != nil {
		defer s.SSELimiter.Release(ip)
	}

	// Enforce max SSE connection duration to prevent indefinite resource consumption.
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(MaxSSEDurationSeconds)*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	sendEvent := func(event string, payload interface{}) {
		data, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flush()
	}

	// Send any existing logs — try executor first for active runs
	sentCount := 0
	var logs []LogEntry
	if s.Executor != nil && !isTerminalStatus(run.Status) {
		executorLogs, err := s.Executor.GetLogs(ctx, runID)
		if err == nil {
			logs = executorLogs
		}
	}
	if logs == nil {
		dbLogs, _ := s.Runs.GetRunLogs(ctx, runID)
		logs = dbLogs
	}
	for _, entry := range logs {
		sendEvent("log", entry)
		sentCount++
	}

	// If already terminal, send status and close
	if isTerminalStatus(run.Status) {
		sendEvent("status", map[string]interface{}{"status": run.Status})
		return
	}

	// Poll for new logs while run is active
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Connection closed by client disconnect or max duration timeout.
			// Send an error event so the client knows the stream ended abnormally
			// (as opposed to a clean "status" event for completed runs).
			if ctx.Err() == context.DeadlineExceeded {
				sendEvent("error", map[string]string{
					"code":    "TIMEOUT",
					"message": "SSE connection closed: maximum duration exceeded",
				})
			} else {
				sendEvent("error", map[string]string{
					"code":    "DISCONNECTED",
					"message": "SSE connection closed",
				})
			}
			return
		case <-ticker.C:
			// Fetch latest logs — try executor for active runs
			var pollLogs []LogEntry
			if s.Executor != nil {
				executorLogs, err := s.Executor.GetLogs(ctx, runID)
				if err == nil {
					pollLogs = executorLogs
				}
			}
			if pollLogs == nil {
				dbLogs, err := s.Runs.GetRunLogs(ctx, runID)
				if err != nil {
					continue
				}
				pollLogs = dbLogs
			}

			// Send only new logs (beyond what we've already sent)
			for i := sentCount; i < len(pollLogs); i++ {
				sendEvent("log", pollLogs[i])
				sentCount++
			}

			// Check if run has finished
			run, err := s.Runs.GetRun(ctx, runID)
			if err != nil {
				continue
			}
			if run != nil && isTerminalStatus(run.Status) {
				sendEvent("status", map[string]interface{}{"status": run.Status})
				return
			}
		}
	}
}
