// Package executor provides the pipeline execution dispatch layer.
// RoundRobinExecutor distributes pipeline submissions across multiple runner
// replicas, failing over to the next runner when one returns RESOURCE_EXHAUSTED.
package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
)

// RoundRobinExecutor distributes pipeline submissions across multiple
// WarmPoolExecutor instances using round-robin selection with
// RESOURCE_EXHAUSTED failover.
//
// When a runner returns RESOURCE_EXHAUSTED, the executor tries the next
// runner in the pool. If all runners are exhausted, it returns ErrRunnerBusy
// so the scheduler can retry on the next tick.
//
// Non-submission RPCs (Cancel, GetLogs, Preview, ValidatePipeline) are
// sent to the first available runner since they are lightweight read operations.
type RoundRobinExecutor struct {
	executors []*WarmPoolExecutor
	counter   atomic.Uint64
}

// NewRoundRobinExecutor creates an executor that distributes work across
// multiple runner addresses. Each address gets its own WarmPoolExecutor
// with independent connection and active run tracking.
//
// Panics if addrs is empty — the caller must provide at least one runner address.
func NewRoundRobinExecutor(addrs []string, runs api.RunStore, httpClient ...*http.Client) *RoundRobinExecutor {
	if len(addrs) == 0 {
		panic("roundrobin: at least one runner address is required")
	}

	executors := make([]*WarmPoolExecutor, len(addrs))
	for i, addr := range addrs {
		executors[i] = NewWarmPoolExecutor(addr, runs, httpClient...)
	}

	slog.Info("round-robin executor initialized", "runners", len(addrs), "addrs", strings.Join(addrs, ","))

	return &RoundRobinExecutor{
		executors: executors,
	}
}

// newRoundRobinExecutorFromPool creates a round-robin executor from
// pre-built WarmPoolExecutor instances (for testing).
func newRoundRobinExecutorFromPool(executors []*WarmPoolExecutor) *RoundRobinExecutor {
	return &RoundRobinExecutor{
		executors: executors,
	}
}

// next returns the index of the next executor to try using atomic round-robin.
func (rr *RoundRobinExecutor) next() int {
	n := rr.counter.Add(1)
	return int((n - 1) % uint64(len(rr.executors)))
}

// Submit dispatches a pipeline run to the next runner in round-robin order.
// If the selected runner returns RESOURCE_EXHAUSTED, tries each subsequent
// runner. Returns ErrRunnerBusy only when ALL runners are exhausted.
func (rr *RoundRobinExecutor) Submit(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error {
	start := rr.next()
	n := len(rr.executors)

	for attempt := 0; attempt < n; attempt++ {
		idx := (start + attempt) % n
		err := rr.executors[idx].Submit(ctx, run, pipeline)
		if err == nil {
			return nil
		}

		// If this runner is busy, try the next one
		if errors.Is(err, ErrRunnerBusy) {
			slog.Info("runner busy, trying next",
				"runner_index", idx,
				"attempt", attempt+1,
				"total_runners", n,
				"run_id", run.ID,
			)
			continue
		}

		// Non-capacity error — return immediately (run already marked failed by WarmPoolExecutor)
		return err
	}

	// All runners exhausted
	return fmt.Errorf("all %d runners at capacity: %w", n, ErrRunnerBusy)
}

// Cancel forwards the cancel request to all executors since we don't track
// which runner owns which run at this level.
func (rr *RoundRobinExecutor) Cancel(ctx context.Context, runID string) error {
	var lastErr error
	for _, exec := range rr.executors {
		err := exec.Cancel(ctx, runID)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// GetLogs tries to fetch logs from each executor until one succeeds.
func (rr *RoundRobinExecutor) GetLogs(ctx context.Context, runID string) ([]api.LogEntry, error) {
	var lastErr error
	for _, exec := range rr.executors {
		logs, err := exec.GetLogs(ctx, runID)
		if err == nil {
			return logs, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// Preview sends the preview request to the next runner in round-robin order.
// Preview is a stateless operation so any runner can handle it.
func (rr *RoundRobinExecutor) Preview(ctx context.Context, pipeline *domain.Pipeline, limit int, sampleFiles []string, code string) (*api.PreviewResult, error) {
	idx := rr.next()
	return rr.executors[idx].Preview(ctx, pipeline, limit, sampleFiles, code)
}

// ValidatePipeline sends the validation request to the next runner in round-robin order.
// Validation is a stateless operation so any runner can handle it.
func (rr *RoundRobinExecutor) ValidatePipeline(ctx context.Context, pipeline *domain.Pipeline) (*api.ValidationResult, error) {
	idx := rr.next()
	return rr.executors[idx].ValidatePipeline(ctx, pipeline)
}

// Start begins the background polling goroutine on each underlying executor.
func (rr *RoundRobinExecutor) Start(ctx context.Context) {
	for _, exec := range rr.executors {
		exec.Start(ctx)
	}
}

// Stop cancels all background goroutines and waits for them to finish.
func (rr *RoundRobinExecutor) Stop() {
	for _, exec := range rr.executors {
		exec.Stop()
	}
}

// SetLandingZones sets the landing zone store on all underlying executors.
func (rr *RoundRobinExecutor) SetLandingZones(lz api.LandingZoneStore) {
	for _, exec := range rr.executors {
		exec.LandingZones = lz
	}
}

// SetOnRunComplete sets the run completion callback on all underlying executors.
func (rr *RoundRobinExecutor) SetOnRunComplete(fn func(ctx context.Context, run *domain.Run, status domain.RunStatus)) {
	for _, exec := range rr.executors {
		exec.OnRunComplete = fn
	}
}

// ParseRunnerAddrs splits a comma-separated runner address string into
// individual addresses, trimming whitespace. Returns nil if the input is empty.
func ParseRunnerAddrs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	addrs := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			addrs = append(addrs, p)
		}
	}
	return addrs
}
