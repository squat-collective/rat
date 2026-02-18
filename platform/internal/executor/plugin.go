// PluginExecutor dispatches pipeline runs to a Pro executor plugin via ConnectRPC.
// It implements the same api.Executor interface as WarmPoolExecutor but delegates
// to an ExecutorService plugin (e.g., ContainerExecutor) instead of the runner directly.
package executor

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	executorv1 "github.com/rat-data/rat/platform/gen/executor/v1"
	"github.com/rat-data/rat/platform/gen/executor/v1/executorv1connect"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
)

// PluginExecutor dispatches pipeline runs to an ExecutorService plugin via ConnectRPC.
// It maintains an in-memory map of active runs and polls for status updates.
type PluginExecutor struct {
	executor      executorv1connect.ExecutorServiceClient
	runs          api.RunStore
	OnRunComplete func(ctx context.Context, run *domain.Run, status domain.RunStatus) // optional callback
	mu            sync.Mutex
	active        map[string]*domain.Run
	pollInterval  time.Duration
	cancel        context.CancelFunc
	done          chan struct{}
}

// NewPluginExecutor creates an executor that talks to the executor plugin at the given address.
// Pass a TLS-enabled http.Client for encrypted transport.
func NewPluginExecutor(addr string, runs api.RunStore, httpClient ...*http.Client) *PluginExecutor {
	var c *http.Client
	if len(httpClient) > 0 && httpClient[0] != nil {
		c = httpClient[0]
	}
	if c == nil {
		c = http.DefaultClient
	}
	client := executorv1connect.NewExecutorServiceClient(
		c,
		addr,
	)
	return &PluginExecutor{
		executor:     client,
		runs:         runs,
		active:       make(map[string]*domain.Run),
		pollInterval: 5 * time.Second,
	}
}

// newPluginExecutorWithClient creates an executor with an injected client (for testing).
func newPluginExecutorWithClient(client executorv1connect.ExecutorServiceClient, runs api.RunStore) *PluginExecutor {
	return &PluginExecutor{
		executor:     client,
		runs:         runs,
		active:       make(map[string]*domain.Run),
		pollInterval: 5 * time.Second,
	}
}

// Submit sends a pipeline run to the executor plugin.
// On success, updates the run status to "running" and tracks it in the active map.
// On failure, updates the run status to "failed".
func (e *PluginExecutor) Submit(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error {
	req := connect.NewRequest(&executorv1.SubmitRequest{
		RunId:        run.ID.String(),
		Namespace:    pipeline.Namespace,
		Layer:        domainLayerToProto(pipeline.Layer),
		PipelineName: pipeline.Name,
		Trigger:      run.Trigger,
		S3Credentials: s3OverridesToProto(run.S3Overrides),
	})
	propagateRequestID(ctx, req)

	_, err := e.executor.Submit(ctx, req)
	if err != nil {
		errMsg := fmt.Sprintf("executor plugin unavailable: %v", err)
		_ = e.runs.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusFailed, &errMsg, nil, nil)
		return fmt.Errorf("submit pipeline: %w", err)
	}

	if err := e.runs.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusRunning, nil, nil, nil); err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	run.Status = domain.RunStatusRunning

	e.mu.Lock()
	e.active[run.ID.String()] = run
	e.mu.Unlock()

	return nil
}

// Cancel tells the executor plugin to cancel a run and removes it from the active map.
func (e *PluginExecutor) Cancel(ctx context.Context, runID string) error {
	req := connect.NewRequest(&commonv1.CancelRunRequest{
		RunId: runID,
	})
	propagateRequestID(ctx, req)

	_, err := e.executor.Cancel(ctx, req)
	if err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}

	e.mu.Lock()
	delete(e.active, runID)
	e.mu.Unlock()

	return nil
}

// GetLogs is not implemented for plugin executor — logs come from the container runtime.
func (e *PluginExecutor) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, fmt.Errorf("not available")
}

// Preview is not available for plugin executors — preview runs on the warm pool runner.
func (e *PluginExecutor) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	return nil, fmt.Errorf("preview not available for plugin executor")
}

// ValidatePipeline is not available for plugin executors — validation runs on the warm pool runner.
func (e *PluginExecutor) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return nil, fmt.Errorf("validation not available for plugin executor")
}

// Start begins the background goroutine that polls for run status updates.
func (e *PluginExecutor) Start(ctx context.Context) {
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
func (e *PluginExecutor) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	if e.done != nil {
		<-e.done
	}
}

// poll checks the status of all active runs via the executor plugin and updates the DB.
func (e *PluginExecutor) poll(ctx context.Context) {
	e.mu.Lock()
	ids := make([]string, 0, len(e.active))
	for id := range e.active {
		ids = append(ids, id)
	}
	e.mu.Unlock()

	for _, id := range ids {
		req := connect.NewRequest(&commonv1.GetRunStatusRequest{
			RunId: id,
		})
		propagateRequestID(ctx, req)

		resp, err := e.executor.GetRunStatus(ctx, req)
		if err != nil {
			slog.Warn("poll: failed to get run status from executor plugin", "run_id", id, "error", err)
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

			e.mu.Lock()
			delete(e.active, id)
			e.mu.Unlock()

			slog.Info("run completed", "run_id", id, "status", status,
				"rows_written", resp.Msg.RowsWritten, "duration_ms", resp.Msg.DurationMs)
		}
	}
}

// s3OverridesToProto converts the domain map[string]string S3 overrides to the proto message.
// Returns nil if the map is empty (no overrides).
func s3OverridesToProto(m map[string]string) *commonv1.S3Credentials {
	if len(m) == 0 {
		return nil
	}
	return &commonv1.S3Credentials{
		Endpoint:       m["endpoint"],
		AccessKeyId:    m["access_key_id"],
		SecretAccessKey: m["secret_access_key"],
		Region:         m["region"],
		Bucket:         m["bucket"],
	}
}

