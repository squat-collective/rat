package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- HandleRunStatusCallback tests ---

func TestRunStatusCallback_ValidSuccess_Returns200(t *testing.T) {
	store := newMemoryRunStore()
	run := &domain.Run{Status: domain.RunStatusRunning}
	_ = store.CreateRun(nil, run)

	mock := &mockCallbackExecutor{
		handleFunc: func(_ api.RunStatusUpdate) error { return nil },
	}

	srv := &api.Server{
		Runs:     store,
		Executor: mock,
	}
	router := api.NewRouter(srv)

	body := api.RunStatusUpdate{
		RunID:       run.ID.String(),
		Status:      "success",
		DurationMs:  1234,
		RowsWritten: 42,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/"+run.ID.String()+"/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, mock.called, "HandleStatusCallback should have been called")
}

func TestRunStatusCallback_InvalidStatus_Returns400(t *testing.T) {
	srv := &api.Server{}
	router := api.NewRouter(srv)

	body := api.RunStatusUpdate{
		RunID:  "some-id",
		Status: "running", // not a terminal status
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/some-id/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRunStatusCallback_MismatchedRunID_Returns400(t *testing.T) {
	srv := &api.Server{}
	router := api.NewRouter(srv)

	body := api.RunStatusUpdate{
		RunID:  "different-id",
		Status: "success",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/url-id/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRunStatusCallback_InvalidJSON_Returns400(t *testing.T) {
	srv := &api.Server{}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/some-id/status", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRunStatusCallback_ExecutorWithoutReceiver_Returns200(t *testing.T) {
	// Executor that does NOT implement StatusCallbackReceiver
	mock := &mockPlainExecutor{}
	srv := &api.Server{
		Executor: mock,
	}
	router := api.NewRouter(srv)

	body := api.RunStatusUpdate{
		RunID:  "some-id",
		Status: "success",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/some-id/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Should accept gracefully even without StatusCallbackReceiver
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRunStatusCallback_EmptyBodyRunID_UsesURLParam(t *testing.T) {
	mock := &mockCallbackExecutor{
		handleFunc: func(_ api.RunStatusUpdate) error {
			return nil
		},
	}

	srv := &api.Server{
		Executor: mock,
	}
	router := api.NewRouter(srv)

	// Body with empty run_id — should use URL param
	body := api.RunStatusUpdate{
		RunID:  "", // empty — will be filled from URL
		Status: "failed",
		Error:  "OOM",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/my-run-id/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.True(t, mock.called)
	assert.Equal(t, "my-run-id", mock.lastUpdate.RunID)
}

func TestRunStatusCallback_FailedStatusWithError_ForwardsError(t *testing.T) {
	mock := &mockCallbackExecutor{
		handleFunc: func(_ api.RunStatusUpdate) error { return nil },
	}

	srv := &api.Server{
		Executor: mock,
	}
	router := api.NewRouter(srv)

	body := api.RunStatusUpdate{
		RunID:  "run-123",
		Status: "failed",
		Error:  "DuckDB OOM on large dataset",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/run-123/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.True(t, mock.called)
	assert.Equal(t, "failed", mock.lastUpdate.Status)
	assert.Equal(t, "DuckDB OOM on large dataset", mock.lastUpdate.Error)
}

func TestRunStatusCallback_NilExecutor_Returns200(t *testing.T) {
	srv := &api.Server{
		Executor: nil,
	}
	router := api.NewRouter(srv)

	body := api.RunStatusUpdate{
		RunID:  "run-123",
		Status: "success",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/run-123/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Nil executor → no StatusCallbackReceiver → accept gracefully
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- Mock executors ---

// mockCallbackExecutor implements both Executor and StatusCallbackReceiver.
type mockCallbackExecutor struct {
	mockPlainExecutor
	handleFunc func(update api.RunStatusUpdate) error
	called     bool
	lastUpdate api.RunStatusUpdate
}

func (m *mockCallbackExecutor) HandleStatusCallback(_ context.Context, update api.RunStatusUpdate) error {
	m.called = true
	m.lastUpdate = update
	if m.handleFunc != nil {
		return m.handleFunc(update)
	}
	return nil
}

// mockPlainExecutor implements Executor but NOT StatusCallbackReceiver.
type mockPlainExecutor struct{}

func (m *mockPlainExecutor) Submit(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error {
	return nil
}
func (m *mockPlainExecutor) Cancel(_ context.Context, _ string) error { return nil }
func (m *mockPlainExecutor) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}
func (m *mockPlainExecutor) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	return nil, nil
}
func (m *mockPlainExecutor) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return nil, nil
}
