// Package executor provides the pipeline execution dispatch layer.
// WarmPoolExecutor is the community-edition executor that dispatches runs
// to a single runner service via ConnectRPC.
package executor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	runnerv1 "github.com/rat-data/rat/platform/gen/runner/v1"
	"github.com/rat-data/rat/platform/gen/runner/v1/runnerv1connect"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/arrowutil"
	"github.com/rat-data/rat/platform/internal/domain"
	"golang.org/x/net/http2"
)

// propagateRequestID copies the request ID from the context (set by the RequestID
// middleware) into the ConnectRPC request header so downstream services (runner, ratq)
// can correlate logs and traces back to the original HTTP request.
func propagateRequestID[T any](ctx context.Context, req *connect.Request[T]) {
	if id := api.RequestIDFromContext(ctx); id != "" {
		req.Header().Set("X-Request-ID", id)
	}
}

// ErrRunnerBusy is returned when the runner rejects a submission because it has
// reached its maximum concurrent run limit (gRPC RESOURCE_EXHAUSTED).
// Callers (e.g. the scheduler) should treat this as transient and retry later
// instead of marking the run as permanently failed.
var ErrRunnerBusy = errors.New("runner at capacity")

// FallbackPollInterval is the reduced polling frequency used as a safety net
// when push-based status callbacks are enabled. The runner pushes status changes
// immediately on completion; polling at 60s catches any missed callbacks (e.g.,
// network partitions, runner crashes).
const FallbackPollInterval = 60 * time.Second

// WarmPoolExecutor dispatches pipeline runs to a ConnectRPC runner service.
// It maintains an in-memory map of active runs. Status updates are primarily
// received via push callbacks from the runner (HandleStatusCallback). Polling
// at 60s serves as a fallback safety net for missed callbacks.
type WarmPoolExecutor struct {
	runner        runnerv1connect.RunnerServiceClient
	runs          api.RunStore
	LandingZones  api.LandingZoneStore // optional — set to clean up files after archive
	OnRunComplete func(ctx context.Context, run *domain.Run, status domain.RunStatus) // optional callback
	mu            sync.Mutex
	active        map[string]*domain.Run // ratd run_id → Run
	runnerIDs     map[string]string      // ratd run_id → runner run_id
	pollInterval  time.Duration
	cancel        context.CancelFunc
	done          chan struct{}
}

// NewWarmPoolExecutor creates an executor that talks to the runner at the given address.
// Uses h2c (HTTP/2 cleartext) by default. Pass a TLS-enabled http.Client for encrypted transport.
func NewWarmPoolExecutor(runnerAddr string, runs api.RunStore, httpClient ...*http.Client) *WarmPoolExecutor {
	var c *http.Client
	if len(httpClient) > 0 && httpClient[0] != nil {
		c = httpClient[0]
	}
	if c == nil {
		c = defaultH2CClient()
	}
	client := runnerv1connect.NewRunnerServiceClient(
		c,
		runnerAddr,
		connect.WithGRPC(),
	)
	return &WarmPoolExecutor{
		runner:       client,
		runs:         runs,
		active:       make(map[string]*domain.Run),
		runnerIDs:    make(map[string]string),
		pollInterval: FallbackPollInterval,
	}
}

// defaultH2CClient creates an HTTP/2 cleartext client.
func defaultH2CClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}
}

// newWarmPoolExecutorWithClient creates an executor with an injected runner client (for testing).
func newWarmPoolExecutorWithClient(client runnerv1connect.RunnerServiceClient, runs api.RunStore) *WarmPoolExecutor {
	return &WarmPoolExecutor{
		runner:       client,
		runs:         runs,
		active:       make(map[string]*domain.Run),
		runnerIDs:    make(map[string]string),
		pollInterval: FallbackPollInterval,
	}
}

// Submit sends a pipeline run to the runner service.
// On success, updates the run status to "running" and tracks it in the active map.
// On failure, updates the run status to "failed".
func (e *WarmPoolExecutor) Submit(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error {
	req := connect.NewRequest(&runnerv1.SubmitPipelineRequest{
		Namespace:         pipeline.Namespace,
		Layer:             domainLayerToProto(pipeline.Layer),
		PipelineName:      pipeline.Name,
		Trigger:           run.Trigger,
		PublishedVersions: pipeline.PublishedVersions,
		RunId:             run.ID.String(),
	})
	propagateRequestID(ctx, req)

	resp, err := e.runner.SubmitPipeline(ctx, req)
	if err != nil {
		// RESOURCE_EXHAUSTED means the runner is at capacity — don't mark
		// the run as failed. Return ErrRunnerBusy so the scheduler can leave
		// the run in pending state and retry on the next tick.
		if connectErr := new(connect.Error); errors.As(err, &connectErr) && connectErr.Code() == connect.CodeResourceExhausted {
			slog.Warn("runner at capacity, will retry", "run_id", run.ID, "detail", connectErr.Message())
			return fmt.Errorf("submit pipeline: %w", ErrRunnerBusy)
		}

		// Runner unavailable for other reasons — mark run as failed
		errMsg := fmt.Sprintf("runner unavailable: %v", err)
		_ = e.runs.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusFailed, &errMsg, nil, nil)
		return fmt.Errorf("submit pipeline: %w", err)
	}

	// Mark as running and track — map ratd run_id to runner run_id for polling
	if err := e.runs.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusRunning, nil, nil, nil); err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	run.Status = domain.RunStatusRunning

	runnerRunID := resp.Msg.RunId
	e.mu.Lock()
	e.active[run.ID.String()] = run
	e.runnerIDs[run.ID.String()] = runnerRunID
	e.mu.Unlock()

	return nil
}

// Cancel tells the runner to cancel a run and updates DB status.
func (e *WarmPoolExecutor) Cancel(ctx context.Context, runID string) error {
	e.mu.Lock()
	runnerID, ok := e.runnerIDs[runID]
	e.mu.Unlock()
	if !ok {
		runnerID = runID
	}

	req := connect.NewRequest(&commonv1.CancelRunRequest{
		RunId: runnerID,
	})
	propagateRequestID(ctx, req)

	_, err := e.runner.CancelRun(ctx, req)
	if err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}

	e.mu.Lock()
	delete(e.active, runID)
	delete(e.runnerIDs, runID)
	e.mu.Unlock()

	return nil
}

// Start begins the background goroutine that polls for run status updates.
func (e *WarmPoolExecutor) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	e.done = make(chan struct{})

	go func() {
		defer close(e.done)
		ticker := time.NewTicker(e.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.poll(ctx)
			}
		}
	}()
}

// Stop cancels the background goroutine and waits for it to finish.
func (e *WarmPoolExecutor) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	if e.done != nil {
		<-e.done
	}
}

// poll checks the status of all active runs and updates the DB for terminal states.
func (e *WarmPoolExecutor) poll(ctx context.Context) {
	e.mu.Lock()
	ids := make([]string, 0, len(e.active))
	for id := range e.active {
		ids = append(ids, id)
	}
	e.mu.Unlock()

	for _, id := range ids {
		// Use runner's run_id for polling (runner generates its own ID)
		e.mu.Lock()
		runnerID, ok := e.runnerIDs[id]
		e.mu.Unlock()
		if !ok {
			runnerID = id // fallback
		}

		req := connect.NewRequest(&commonv1.GetRunStatusRequest{
			RunId: runnerID,
		})
		propagateRequestID(ctx, req)

		resp, err := e.runner.GetRunStatus(ctx, req)
		if err != nil {
			slog.Warn("poll: failed to get run status", "run_id", id, "runner_id", runnerID, "error", err)
			continue
		}

		status := protoStatusToDomain(resp.Msg.Status)
		if status == domain.RunStatusSuccess || status == domain.RunStatusFailed {
			var errMsg *string
			if resp.Msg.Error != "" {
				e := resp.Msg.Error
				errMsg = &e
			}
			var durationMs, rowsWritten *int64
			if resp.Msg.DurationMs > 0 {
				v := resp.Msg.DurationMs
				durationMs = &v
			}
			if resp.Msg.RowsWritten >= 0 {
				v := resp.Msg.RowsWritten
				rowsWritten = &v
			}
			if err := e.runs.UpdateRunStatus(ctx, id, status, errMsg, durationMs, rowsWritten); err != nil {
				slog.Error("poll: failed to update run status", "run_id", id, "error", err)
				continue
			}

			// Notify listeners (e.g., pipeline_success triggers).
			// Use a fresh context with timeout — the parent poll context may be
			// cancelled (e.g., during shutdown) before the callback completes.
			if e.OnRunComplete != nil {
				e.mu.Lock()
				run := e.active[id]
				e.mu.Unlock()
				if run != nil {
					go func(r *domain.Run, s domain.RunStatus) {
						cbCtx, cbCancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cbCancel()
						e.OnRunComplete(cbCtx, r, s)
					}(run, status)
				}
			}

			// Persist logs before removing from active tracking
			if logs, err := e.GetLogs(ctx, id); err == nil && len(logs) > 0 {
				if err := e.runs.SaveRunLogs(ctx, id, logs); err != nil {
					slog.Error("poll: failed to save run logs", "run_id", id, "error", err)
				}
			}

			// Clean up landing zone file records after successful archive
			if status == domain.RunStatusSuccess {
				if zones := resp.Msg.ArchivedLandingZones; len(zones) > 0 {
					e.cleanupArchivedZones(ctx, zones)
				} else {
					// Fallback: legacy trigger-based cleanup
					e.mu.Lock()
					run := e.active[id]
					e.mu.Unlock()
					if run != nil {
						e.cleanupLandingFiles(ctx, run.Trigger)
					}
				}
			}

			e.mu.Lock()
			delete(e.active, id)
			delete(e.runnerIDs, id)
			e.mu.Unlock()

			slog.Info("run completed", "run_id", id, "status", status)
		}
	}
}

// cleanupLandingFiles deletes landing zone file DB records after the runner
// has archived them to _processed/ in S3. Trigger format:
// "trigger:landing_zone_upload:default/raw-uploads"
func (e *WarmPoolExecutor) cleanupLandingFiles(ctx context.Context, trigger string) {
	if e.LandingZones == nil {
		return
	}
	// Parse trigger string: "trigger:landing_zone_upload:{ns}/{zone}"
	const prefix = "trigger:landing_zone_upload:"
	if !strings.HasPrefix(trigger, prefix) {
		return
	}
	parts := strings.SplitN(trigger[len(prefix):], "/", 2)
	if len(parts) != 2 {
		return
	}
	ns, zoneName := parts[0], parts[1]

	zone, err := e.LandingZones.GetZone(ctx, ns, zoneName)
	if err != nil || zone == nil {
		return
	}
	files, err := e.LandingZones.ListFiles(ctx, zone.ID)
	if err != nil {
		return
	}
	for _, f := range files {
		if err := e.LandingZones.DeleteFile(ctx, f.ID); err != nil {
			slog.Warn("poll: failed to delete landing file record", "file_id", f.ID, "error", err)
		}
	}
	if len(files) > 0 {
		slog.Info("cleaned up landing file records", "zone", ns+"/"+zoneName, "count", len(files))
	}
}

// cleanupArchivedZones deletes landing zone file DB records for zones that
// the runner explicitly reported as archived. Zone format: "{ns}/{zone}".
func (e *WarmPoolExecutor) cleanupArchivedZones(ctx context.Context, zones []string) {
	if e.LandingZones == nil {
		return
	}
	for _, z := range zones {
		parts := strings.SplitN(z, "/", 2)
		if len(parts) != 2 {
			continue
		}
		ns, zoneName := parts[0], parts[1]

		zone, err := e.LandingZones.GetZone(ctx, ns, zoneName)
		if err != nil || zone == nil {
			continue
		}
		files, err := e.LandingZones.ListFiles(ctx, zone.ID)
		if err != nil {
			continue
		}
		for _, f := range files {
			if err := e.LandingZones.DeleteFile(ctx, f.ID); err != nil {
				slog.Warn("poll: failed to delete landing file record", "file_id", f.ID, "error", err)
			}
		}
		if len(files) > 0 {
			slog.Info("cleaned up landing file records", "zone", z, "count", len(files))
		}
	}
}

// GetLogs fetches logs from the runner for an active run via StreamLogs RPC.
func (e *WarmPoolExecutor) GetLogs(ctx context.Context, runID string) ([]api.LogEntry, error) {
	e.mu.Lock()
	runnerID, ok := e.runnerIDs[runID]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("run %s not tracked (may have completed)", runID)
	}

	req := connect.NewRequest(&commonv1.StreamLogsRequest{
		RunId:  runnerID,
		Follow: false,
	})
	propagateRequestID(ctx, req)

	stream, err := e.runner.StreamLogs(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("stream logs: %w", err)
	}
	defer stream.Close()

	var logs []api.LogEntry
	for stream.Receive() {
		entry := stream.Msg()
		ts := ""
		if entry.Timestamp != nil {
			ts = time.Unix(entry.Timestamp.Seconds, int64(entry.Timestamp.Nanos)).UTC().Format(time.RFC3339)
		}
		logs = append(logs, api.LogEntry{
			Timestamp: ts,
			Level:     entry.Level,
			Message:   entry.Message,
		})
	}
	if err := stream.Err(); err != nil {
		return logs, fmt.Errorf("stream logs: %w", err)
	}
	return logs, nil
}

// Preview calls the runner's PreviewPipeline RPC and converts the response.
func (e *WarmPoolExecutor) Preview(ctx context.Context, pipeline *domain.Pipeline, limit int, sampleFiles []string, code string) (*api.PreviewResult, error) {
	req := connect.NewRequest(&runnerv1.PreviewPipelineRequest{
		Namespace:    pipeline.Namespace,
		Layer:        domainLayerToProto(pipeline.Layer),
		PipelineName: pipeline.Name,
		PreviewLimit: int32(limit),
		SampleFiles:  sampleFiles,
		Code:         code,
		PipelineType: pipeline.Type,
	})
	propagateRequestID(ctx, req)

	resp, err := e.runner.PreviewPipeline(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("preview pipeline: %w", err)
	}

	msg := resp.Msg

	// Convert columns
	columns := make([]api.QueryColumn, 0, len(msg.Columns))
	for _, c := range msg.Columns {
		columns = append(columns, api.QueryColumn{
			Name: c.Name,
			Type: c.Type,
		})
	}

	// Convert Arrow IPC to rows
	rows, err := arrowutil.IPCToRows(msg.ArrowIpc)
	if err != nil {
		return nil, fmt.Errorf("deserialize arrow: %w", err)
	}

	// Convert phases
	phases := make([]api.PhaseProfile, 0, len(msg.Phases))
	for _, p := range msg.Phases {
		phases = append(phases, api.PhaseProfile{
			Name:       p.Name,
			DurationMs: p.DurationMs,
			Metadata:   p.Metadata,
		})
	}

	// Convert logs
	logs := make([]api.LogEntry, 0, len(msg.Logs))
	for _, entry := range msg.Logs {
		ts := ""
		if entry.Timestamp != nil {
			ts = time.Unix(entry.Timestamp.Seconds, int64(entry.Timestamp.Nanos)).UTC().Format(time.RFC3339)
		}
		logs = append(logs, api.LogEntry{
			Timestamp: ts,
			Level:     entry.Level,
			Message:   entry.Message,
		})
	}

	result := &api.PreviewResult{
		Columns:       columns,
		Rows:          rows,
		TotalRowCount: msg.TotalRowCount,
		Phases:        phases,
		ExplainOutput: msg.ExplainOutput,
		MemoryPeak:    msg.MemoryPeakBytes,
		Logs:          logs,
		Error:         msg.Error,
		Warnings:      msg.Warnings,
	}

	if result.Rows == nil {
		result.Rows = []map[string]interface{}{}
	}
	if result.Warnings == nil {
		result.Warnings = []string{}
	}

	return result, nil
}

// ValidatePipeline calls the runner's ValidatePipeline RPC and converts the response.
func (e *WarmPoolExecutor) ValidatePipeline(ctx context.Context, pipeline *domain.Pipeline) (*api.ValidationResult, error) {
	req := connect.NewRequest(&runnerv1.ValidatePipelineRequest{
		Namespace:    pipeline.Namespace,
		Layer:        domainLayerToProto(pipeline.Layer),
		PipelineName: pipeline.Name,
	})
	propagateRequestID(ctx, req)

	resp, err := e.runner.ValidatePipeline(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("validate pipeline: %w", err)
	}

	msg := resp.Msg
	files := make([]api.FileValidation, 0, len(msg.Files))
	for _, f := range msg.Files {
		files = append(files, api.FileValidation{
			Path:     f.Path,
			Valid:    f.Valid,
			Errors:   f.Errors,
			Warnings: f.Warnings,
		})
	}

	return &api.ValidationResult{
		Valid: msg.Valid,
		Files: files,
	}, nil
}

// domainLayerToProto converts domain.Layer to proto Layer enum.
func domainLayerToProto(l domain.Layer) commonv1.Layer {
	switch l {
	case domain.LayerBronze:
		return commonv1.Layer_LAYER_BRONZE
	case domain.LayerSilver:
		return commonv1.Layer_LAYER_SILVER
	case domain.LayerGold:
		return commonv1.Layer_LAYER_GOLD
	default:
		return commonv1.Layer_LAYER_UNSPECIFIED
	}
}

// protoStatusToDomain converts proto RunStatus to domain.RunStatus.
func protoStatusToDomain(s commonv1.RunStatus) domain.RunStatus {
	switch s {
	case commonv1.RunStatus_RUN_STATUS_PENDING:
		return domain.RunStatusPending
	case commonv1.RunStatus_RUN_STATUS_RUNNING:
		return domain.RunStatusRunning
	case commonv1.RunStatus_RUN_STATUS_SUCCESS:
		return domain.RunStatusSuccess
	case commonv1.RunStatus_RUN_STATUS_FAILED:
		return domain.RunStatusFailed
	default:
		return domain.RunStatusPending
	}
}

// HandleStatusCallback processes a push-based status update from the runner.
// This is the primary path for status updates — the runner POSTs here when a
// run reaches a terminal state. The poll loop at 60s serves as a fallback.
//
// This method performs the same actions as the poll loop: update Postgres,
// persist logs, clean up landing zones, fire OnRunComplete, and remove
// the run from the active map.
func (e *WarmPoolExecutor) HandleStatusCallback(ctx context.Context, update api.RunStatusUpdate) error {
	id := update.RunID

	// Check if this run is tracked. If not, it may have already been
	// cleaned up by the poll fallback — accept idempotently.
	e.mu.Lock()
	_, tracked := e.active[id]
	e.mu.Unlock()
	if !tracked {
		slog.Info("callback: run not in active map (already processed or unknown)", "run_id", id)
		return nil
	}

	status := callbackStatusToDomain(update.Status)
	if status != domain.RunStatusSuccess && status != domain.RunStatusFailed && status != domain.RunStatusCancelled {
		return fmt.Errorf("callback: unexpected status %q for run %s", update.Status, id)
	}

	// Update Postgres (single source of truth)
	var errMsg *string
	if update.Error != "" {
		errMsg = &update.Error
	}
	var durationMs, rowsWritten *int64
	if update.DurationMs > 0 {
		v := update.DurationMs
		durationMs = &v
	}
	if update.RowsWritten >= 0 {
		v := update.RowsWritten
		rowsWritten = &v
	}
	if err := e.runs.UpdateRunStatus(ctx, id, status, errMsg, durationMs, rowsWritten); err != nil {
		return fmt.Errorf("callback: update run status: %w", err)
	}

	// Notify listeners (e.g., pipeline_success triggers).
	// Use a fresh context with timeout — the caller's HTTP request context will
	// be cancelled after the response is sent, but the callback may need more time.
	if e.OnRunComplete != nil {
		e.mu.Lock()
		run := e.active[id]
		e.mu.Unlock()
		if run != nil {
			go func(r *domain.Run, s domain.RunStatus) {
				cbCtx, cbCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cbCancel()
				e.OnRunComplete(cbCtx, r, s)
			}(run, status)
		}
	}

	// Persist logs before removing from active tracking
	if logs, err := e.GetLogs(ctx, id); err == nil && len(logs) > 0 {
		if err := e.runs.SaveRunLogs(ctx, id, logs); err != nil {
			slog.Error("callback: failed to save run logs", "run_id", id, "error", err)
		}
	}

	// Clean up landing zone file records after successful archive
	if status == domain.RunStatusSuccess {
		if zones := update.ArchivedLandingZones; len(zones) > 0 {
			e.cleanupArchivedZones(ctx, zones)
		} else {
			// Fallback: legacy trigger-based cleanup
			e.mu.Lock()
			run := e.active[id]
			e.mu.Unlock()
			if run != nil {
				e.cleanupLandingFiles(ctx, run.Trigger)
			}
		}
	}

	// Remove from active map
	e.mu.Lock()
	delete(e.active, id)
	delete(e.runnerIDs, id)
	e.mu.Unlock()

	slog.Info("callback: run completed", "run_id", id, "status", status)
	return nil
}

// callbackStatusToDomain converts a callback status string to domain.RunStatus.
func callbackStatusToDomain(s string) domain.RunStatus {
	switch s {
	case "success":
		return domain.RunStatusSuccess
	case "failed":
		return domain.RunStatusFailed
	case "cancelled":
		return domain.RunStatusCancelled
	default:
		return domain.RunStatusPending
	}
}
