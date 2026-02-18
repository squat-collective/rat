package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// previewExecutor implements api.Executor with a controllable Preview method.
type previewExecutor struct {
	result      *api.PreviewResult
	err         error
	capturedCode string
}

func (e *previewExecutor) Submit(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error {
	return nil
}
func (e *previewExecutor) Cancel(_ context.Context, _ string) error { return nil }
func (e *previewExecutor) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}
func (e *previewExecutor) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, code string) (*api.PreviewResult, error) {
	e.capturedCode = code
	return e.result, e.err
}
func (e *previewExecutor) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return &api.ValidationResult{Valid: true}, nil
}

func TestHandlePreviewPipeline_Success(t *testing.T) {
	pipelineStore := newMemoryPipelineStore()
	p := &domain.Pipeline{
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
		Type:      "sql",
	}
	require.NoError(t, pipelineStore.CreatePipeline(context.Background(), p))

	exec := &previewExecutor{
		result: &api.PreviewResult{
			Columns: []api.QueryColumn{
				{Name: "id", Type: "INTEGER"},
			},
			Rows:          []map[string]interface{}{{"id": 1}},
			TotalRowCount: 100,
			Phases: []api.PhaseProfile{
				{Name: "detect", DurationMs: 5},
				{Name: "execute", DurationMs: 50},
			},
			ExplainOutput: "SCAN table",
			MemoryPeak:    1048576,
			Logs:          []api.LogEntry{{Level: "info", Message: "preview ok"}},
			Warnings:      []string{},
		},
	}

	srv := &api.Server{
		Pipelines: pipelineStore,
		Executor:  exec,
	}
	r := chi.NewRouter()
	r.Post("/api/v1/pipelines/{namespace}/{layer}/{name}/preview", srv.HandlePreviewPipeline)

	body, _ := json.Marshal(api.PreviewRequest{Limit: 50})
	req := httptest.NewRequest("POST", "/api/v1/pipelines/default/silver/orders/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result api.PreviewResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Equal(t, int64(100), result.TotalRowCount)
	assert.Len(t, result.Columns, 1)
	assert.Len(t, result.Phases, 2)
	assert.Equal(t, "SCAN table", result.ExplainOutput)
}

func TestHandlePreviewPipeline_PipelineNotFound(t *testing.T) {
	srv := &api.Server{
		Pipelines: newMemoryPipelineStore(),
	}
	r := chi.NewRouter()
	r.Post("/api/v1/pipelines/{namespace}/{layer}/{name}/preview", srv.HandlePreviewPipeline)

	req := httptest.NewRequest("POST", "/api/v1/pipelines/default/silver/missing/preview", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandlePreviewPipeline_NoExecutor(t *testing.T) {
	pipelineStore := newMemoryPipelineStore()
	p := &domain.Pipeline{
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
		Type:      "sql",
	}
	require.NoError(t, pipelineStore.CreatePipeline(context.Background(), p))

	srv := &api.Server{
		Pipelines: pipelineStore,
		// Executor is nil
	}
	r := chi.NewRouter()
	r.Post("/api/v1/pipelines/{namespace}/{layer}/{name}/preview", srv.HandlePreviewPipeline)

	req := httptest.NewRequest("POST", "/api/v1/pipelines/default/silver/orders/preview", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandlePreviewPipeline_Error_RedactsInternalDetails(t *testing.T) {
	pipelineStore := newMemoryPipelineStore()
	p := &domain.Pipeline{
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
		Type:      "sql",
	}
	require.NoError(t, pipelineStore.CreatePipeline(context.Background(), p))

	exec := &previewExecutor{
		err: fmt.Errorf("DuckDB error: connection to /tmp/duck.db failed: ENOMEM at 0x7fff1234"),
	}

	srv := &api.Server{
		Pipelines: pipelineStore,
		Executor:  exec,
	}
	r := chi.NewRouter()
	r.Post("/api/v1/pipelines/{namespace}/{layer}/{name}/preview", srv.HandlePreviewPipeline)

	req := httptest.NewRequest("POST", "/api/v1/pipelines/default/silver/orders/preview", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// The response must NOT contain internal error details (DB paths, memory addresses).
	body := w.Body.String()
	assert.NotContains(t, body, "DuckDB")
	assert.NotContains(t, body, "/tmp/duck.db")
	assert.NotContains(t, body, "0x7fff")
	assert.Contains(t, body, "preview execution failed")
}

func TestHandlePreviewPipeline_WithInlineCode(t *testing.T) {
	pipelineStore := newMemoryPipelineStore()
	p := &domain.Pipeline{
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
		Type:      "sql",
	}
	require.NoError(t, pipelineStore.CreatePipeline(context.Background(), p))

	exec := &previewExecutor{
		result: &api.PreviewResult{
			Columns:       []api.QueryColumn{{Name: "x", Type: "INTEGER"}},
			Rows:          []map[string]interface{}{{"x": 1}},
			TotalRowCount: 1,
			Phases:        []api.PhaseProfile{},
			Warnings:      []string{},
		},
	}

	srv := &api.Server{
		Pipelines: pipelineStore,
		Executor:  exec,
	}
	r := chi.NewRouter()
	r.Post("/api/v1/pipelines/{namespace}/{layer}/{name}/preview", srv.HandlePreviewPipeline)

	body, _ := json.Marshal(api.PreviewRequest{Limit: 50, Code: "SELECT 1 AS x"})
	req := httptest.NewRequest("POST", "/api/v1/pipelines/default/silver/orders/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "SELECT 1 AS x", exec.capturedCode)
}
