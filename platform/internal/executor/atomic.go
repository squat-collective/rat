// AtomicExecutor is a thread-safe api.Executor proxy backed by sync/atomic.Value.
// All methods delegate to the currently stored inner executor. Returns an error
// when no executor is loaded. Swaps are extremely rare (plugin register/unregister),
// while reads happen on every HTTP request — atomic.Value gives lock-free reads
// with zero contention on the hot path.
package executor

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
)

// ErrNoExecutor is returned when the AtomicExecutor has no inner executor loaded.
var ErrNoExecutor = fmt.Errorf("no executor available")

// AtomicExecutor wraps an api.Executor with atomic swap semantics.
// It implements both api.Executor and api.StatusCallbackReceiver.
type AtomicExecutor struct {
	inner atomic.Value // stores api.Executor
}

// NewAtomicExecutor creates an empty AtomicExecutor. Call Swap to load an executor.
func NewAtomicExecutor() *AtomicExecutor {
	return &AtomicExecutor{}
}

// Get returns the current inner executor, or nil if empty.
func (a *AtomicExecutor) Get() api.Executor {
	v := a.inner.Load()
	if v == nil {
		return nil
	}
	return v.(api.Executor)
}

// Swap atomically replaces the inner executor and returns the previous one.
// Pass nil to clear the executor.
func (a *AtomicExecutor) Swap(new api.Executor) api.Executor {
	old := a.inner.Swap(new)
	if old == nil {
		return nil
	}
	return old.(api.Executor)
}

// Submit delegates to the inner executor.
func (a *AtomicExecutor) Submit(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error {
	exec := a.Get()
	if exec == nil {
		return ErrNoExecutor
	}
	return exec.Submit(ctx, run, pipeline)
}

// Cancel delegates to the inner executor.
func (a *AtomicExecutor) Cancel(ctx context.Context, runID string) error {
	exec := a.Get()
	if exec == nil {
		return ErrNoExecutor
	}
	return exec.Cancel(ctx, runID)
}

// GetLogs delegates to the inner executor.
func (a *AtomicExecutor) GetLogs(ctx context.Context, runID string) ([]api.LogEntry, error) {
	exec := a.Get()
	if exec == nil {
		return nil, ErrNoExecutor
	}
	return exec.GetLogs(ctx, runID)
}

// Preview delegates to the inner executor.
func (a *AtomicExecutor) Preview(ctx context.Context, pipeline *domain.Pipeline, limit int, sampleFiles []string, code string) (*api.PreviewResult, error) {
	exec := a.Get()
	if exec == nil {
		return nil, ErrNoExecutor
	}
	return exec.Preview(ctx, pipeline, limit, sampleFiles, code)
}

// ValidatePipeline delegates to the inner executor.
func (a *AtomicExecutor) ValidatePipeline(ctx context.Context, pipeline *domain.Pipeline) (*api.ValidationResult, error) {
	exec := a.Get()
	if exec == nil {
		return nil, ErrNoExecutor
	}
	return exec.ValidatePipeline(ctx, pipeline)
}

// HandleStatusCallback delegates to the inner executor if it implements
// api.StatusCallbackReceiver. Returns nil (accepted) if the inner executor
// does not support callbacks — mirrors the graceful fallback in run_callback.go.
func (a *AtomicExecutor) HandleStatusCallback(ctx context.Context, update api.RunStatusUpdate) error {
	exec := a.Get()
	if exec == nil {
		return ErrNoExecutor
	}
	if receiver, ok := exec.(api.StatusCallbackReceiver); ok {
		return receiver.HandleStatusCallback(ctx, update)
	}
	// Inner executor doesn't support callbacks — accept gracefully.
	return nil
}
