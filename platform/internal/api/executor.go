package api

import (
	"context"

	"github.com/rat-data/rat/platform/internal/domain"
)

// PreviewResult holds the response from a pipeline preview execution.
type PreviewResult struct {
	Columns       []QueryColumn            `json:"columns"`
	Rows          []map[string]interface{}  `json:"rows"`
	TotalRowCount int64                    `json:"total_row_count"`
	Phases        []PhaseProfile           `json:"phases"`
	ExplainOutput string                   `json:"explain_output"`
	MemoryPeak    int64                    `json:"memory_peak_bytes"`
	Logs          []LogEntry               `json:"logs"`
	Error         string                   `json:"error,omitempty"`
	Warnings      []string                 `json:"warnings"`
}

// PhaseProfile captures timing for a single execution phase.
type PhaseProfile struct {
	Name       string            `json:"name"`
	DurationMs int64             `json:"duration_ms"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ValidationResult holds the outcome of template validation for a pipeline.
type ValidationResult struct {
	Valid bool             `json:"valid"`
	Files []FileValidation `json:"files"`
}

// FileValidation holds per-file validation results.
type FileValidation struct {
	Path     string   `json:"path"`
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// Executor dispatches pipeline runs to the runner service.
// Implemented by WarmPoolExecutor (community) or plugin executors (pro).
type Executor interface {
	Submit(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error
	Cancel(ctx context.Context, runID string) error
	GetLogs(ctx context.Context, runID string) ([]LogEntry, error)
	Preview(ctx context.Context, pipeline *domain.Pipeline, limit int, sampleFiles []string, code string) (*PreviewResult, error)
	ValidatePipeline(ctx context.Context, pipeline *domain.Pipeline) (*ValidationResult, error)
}

// StatusCallbackReceiver is an optional interface that executors can implement
// to accept push-based run status updates from the runner. When implemented,
// the runner POSTs status changes directly to ratd instead of waiting for the
// next poll cycle (which serves as a fallback only).
//
// This eliminates the N-gRPC-calls-per-interval problem: instead of ratd polling
// every run every 5s, the runner pushes status on completion. Polling is reduced
// to a 60s fallback safety net.
type StatusCallbackReceiver interface {
	// HandleStatusCallback processes a push-based status update from the runner.
	// Called by the internal HTTP handler when the runner POSTs a terminal status.
	HandleStatusCallback(ctx context.Context, update RunStatusUpdate) error
}

// RunStatusUpdate is the JSON payload the runner sends to ratd when a run
// reaches a terminal state (success/failed/cancelled).
type RunStatusUpdate struct {
	RunID                string   `json:"run_id"`
	Status               string   `json:"status"`                          // "success", "failed", "cancelled"
	Error                string   `json:"error,omitempty"`
	DurationMs           int64    `json:"duration_ms,omitempty"`
	RowsWritten          int64    `json:"rows_written"`
	ArchivedLandingZones []string `json:"archived_landing_zones,omitempty"` // "{ns}/{zone}" pairs
}
