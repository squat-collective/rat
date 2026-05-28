package executor

import (
	"context"
	"sync"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock executor (implements api.Executor only) ---

type mockExec struct {
	submitCalled   bool
	cancelCalled   bool
	logsCalled     bool
	previewCalled  bool
	validateCalled bool
}

func (m *mockExec) Submit(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error {
	m.submitCalled = true
	return nil
}

func (m *mockExec) Cancel(_ context.Context, _ string) error {
	m.cancelCalled = true
	return nil
}

func (m *mockExec) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	m.logsCalled = true
	return []api.LogEntry{{Message: "hello"}}, nil
}

func (m *mockExec) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	m.previewCalled = true
	return &api.PreviewResult{}, nil
}

func (m *mockExec) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	m.validateCalled = true
	return &api.ValidationResult{Valid: true}, nil
}

// --- Mock executor with StatusCallbackReceiver ---

type mockCallbackExec struct {
	mockExec
	callbackCalled bool
	lastUpdate     api.RunStatusUpdate
}

func (m *mockCallbackExec) HandleStatusCallback(_ context.Context, update api.RunStatusUpdate) error {
	m.callbackCalled = true
	m.lastUpdate = update
	return nil
}

// --- Tests ---

func TestAtomicExecutor_NilReturnsError(t *testing.T) {
	ae := NewAtomicExecutor()

	assert.Nil(t, ae.Get())

	err := ae.Submit(context.Background(), &domain.Run{}, &domain.Pipeline{})
	assert.ErrorIs(t, err, ErrNoExecutor)

	err = ae.Cancel(context.Background(), "run-1")
	assert.ErrorIs(t, err, ErrNoExecutor)

	_, err = ae.GetLogs(context.Background(), "run-1")
	assert.ErrorIs(t, err, ErrNoExecutor)

	_, err = ae.Preview(context.Background(), &domain.Pipeline{}, 10, nil, "")
	assert.ErrorIs(t, err, ErrNoExecutor)

	_, err = ae.ValidatePipeline(context.Background(), &domain.Pipeline{})
	assert.ErrorIs(t, err, ErrNoExecutor)

	err = ae.HandleStatusCallback(context.Background(), api.RunStatusUpdate{})
	assert.ErrorIs(t, err, ErrNoExecutor)
}

func TestAtomicExecutor_DelegatesToCurrent(t *testing.T) {
	ae := NewAtomicExecutor()
	mock := &mockExec{}
	ae.Swap(mock)

	ctx := context.Background()

	require.NoError(t, ae.Submit(ctx, &domain.Run{}, &domain.Pipeline{}))
	assert.True(t, mock.submitCalled)

	require.NoError(t, ae.Cancel(ctx, "run-1"))
	assert.True(t, mock.cancelCalled)

	logs, err := ae.GetLogs(ctx, "run-1")
	require.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.True(t, mock.logsCalled)

	result, err := ae.Preview(ctx, &domain.Pipeline{}, 10, nil, "")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, mock.previewCalled)

	vr, err := ae.ValidatePipeline(ctx, &domain.Pipeline{})
	require.NoError(t, err)
	assert.True(t, vr.Valid)
	assert.True(t, mock.validateCalled)
}

func TestAtomicExecutor_SwapReturnsOld(t *testing.T) {
	ae := NewAtomicExecutor()

	// First swap from nil.
	old := ae.Swap(&mockExec{})
	assert.Nil(t, old)

	// Second swap returns previous.
	mock1 := &mockExec{}
	ae.Swap(mock1)

	mock2 := &mockExec{}
	old = ae.Swap(mock2)
	assert.Equal(t, mock1, old)

	// Current should be mock2.
	assert.Equal(t, mock2, ae.Get())
}

// noopExec is a race-free executor mock for concurrent tests.
// Unlike mockExec, it does not track calls (no shared mutable state).
type noopExec struct{}

func (n *noopExec) Submit(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error { return nil }
func (n *noopExec) Cancel(_ context.Context, _ string) error                          { return nil }
func (n *noopExec) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error)       { return nil, nil }
func (n *noopExec) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	return &api.PreviewResult{}, nil
}
func (n *noopExec) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return &api.ValidationResult{Valid: true}, nil
}

func TestAtomicExecutor_ConcurrentAccess(t *testing.T) {
	ae := NewAtomicExecutor()
	ae.Swap(&noopExec{})

	var wg sync.WaitGroup
	ctx := context.Background()

	// 50 concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = ae.Submit(ctx, &domain.Run{}, &domain.Pipeline{})
				_ = ae.Cancel(ctx, "run-1")
				_, _ = ae.GetLogs(ctx, "run-1")
				_, _ = ae.Preview(ctx, &domain.Pipeline{}, 10, nil, "")
				_, _ = ae.ValidatePipeline(ctx, &domain.Pipeline{})
			}
		}()
	}

	// 2 concurrent writers.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ae.Swap(&noopExec{})
			}
		}()
	}

	wg.Wait()

	// If we get here without -race detecting issues, the test passes.
	assert.NotNil(t, ae.Get())
}

func TestAtomicExecutor_StatusCallback_Delegation(t *testing.T) {
	ae := NewAtomicExecutor()
	mock := &mockCallbackExec{}
	ae.Swap(mock)

	update := api.RunStatusUpdate{
		RunID:  "run-42",
		Status: "success",
	}

	err := ae.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err)
	assert.True(t, mock.callbackCalled)
	assert.Equal(t, "run-42", mock.lastUpdate.RunID)
}

func TestAtomicExecutor_StatusCallback_GracefulWhenNotSupported(t *testing.T) {
	ae := NewAtomicExecutor()
	// mockExec does NOT implement StatusCallbackReceiver.
	ae.Swap(&mockExec{})

	update := api.RunStatusUpdate{
		RunID:  "run-42",
		Status: "success",
	}

	err := ae.HandleStatusCallback(context.Background(), update)
	assert.NoError(t, err, "should accept gracefully when inner doesn't support callbacks")
}
