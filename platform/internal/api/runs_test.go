package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryRunStore is an in-memory RunStore for tests.
type memoryRunStore struct {
	mu   sync.Mutex
	runs []domain.Run
}

func newMemoryRunStore() *memoryRunStore {
	return &memoryRunStore{}
}

func (m *memoryRunStore) filteredRuns(filter api.RunFilter) []domain.Run {
	var result []domain.Run
	for _, r := range m.runs {
		if filter.Status != "" && string(r.Status) != filter.Status {
			continue
		}
		result = append(result, r)
	}
	return result
}

func (m *memoryRunStore) ListRuns(_ context.Context, filter api.RunFilter) ([]domain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := m.filteredRuns(filter)
	if filter.Limit > 0 {
		if filter.Offset >= len(result) {
			return []domain.Run{}, nil
		}
		end := filter.Offset + filter.Limit
		if end > len(result) {
			end = len(result)
		}
		result = result[filter.Offset:end]
	}
	return result, nil
}

func (m *memoryRunStore) CountRuns(_ context.Context, filter api.RunFilter) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.filteredRuns(filter)), nil
}

func (m *memoryRunStore) GetRun(_ context.Context, runID string) (*domain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, err := uuid.Parse(runID)
	if err != nil {
		return nil, nil
	}
	for _, r := range m.runs {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, nil
}

func (m *memoryRunStore) CreateRun(_ context.Context, run *domain.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	run.ID = uuid.New()
	m.runs = append(m.runs, *run)
	return nil
}

func (m *memoryRunStore) UpdateRunStatus(_ context.Context, runID string, status domain.RunStatus, errMsg *string, _, _ *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, err := uuid.Parse(runID)
	if err != nil {
		return fmt.Errorf("invalid run ID: %w", err)
	}
	for i, r := range m.runs {
		if r.ID == id {
			m.runs[i].Status = status
			m.runs[i].Error = errMsg
			return nil
		}
	}
	// P10-27: Return error on missing run to match production Postgres behavior.
	return fmt.Errorf("run %s not found", runID)
}

func (m *memoryRunStore) DeleteRunsBeyondLimit(_ context.Context, _ uuid.UUID, _ int) (int, error) {
	return 0, nil
}

func (m *memoryRunStore) DeleteRunsOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func (m *memoryRunStore) ListStuckRuns(_ context.Context, _ time.Time) ([]domain.Run, error) {
	return nil, nil
}

func (m *memoryRunStore) LatestRunPerPipeline(_ context.Context, pipelineIDs []uuid.UUID) (map[uuid.UUID]*domain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[uuid.UUID]*domain.Run)
	for _, pid := range pipelineIDs {
		for i := len(m.runs) - 1; i >= 0; i-- {
			if m.runs[i].PipelineID == pid {
				run := m.runs[i]
				result[pid] = &run
				break
			}
		}
	}
	return result, nil
}

func (m *memoryRunStore) SaveRunLogs(_ context.Context, _ string, _ []api.LogEntry) error {
	return nil
}

func (m *memoryRunStore) GetRunLogs(_ context.Context, runID string) ([]api.LogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, err := uuid.Parse(runID)
	if err != nil {
		return nil, nil
	}
	for _, r := range m.runs {
		if r.ID == id {
			return []api.LogEntry{
				{Timestamp: "2026-02-12T14:00:00Z", Level: "info", Message: "Starting pipeline"},
				{Timestamp: "2026-02-12T14:00:01Z", Level: "info", Message: "Pipeline completed"},
			}, nil
		}
	}
	return nil, nil
}

// newRunTestServer creates a Server with both pipeline and run stores.
func newRunTestServer() (*api.Server, *memoryPipelineStore, *memoryRunStore) {
	pipelineStore := newMemoryPipelineStore()
	runStore := newMemoryRunStore()
	srv := &api.Server{
		Pipelines:  pipelineStore,
		Runs:       runStore,
		Namespaces: newMemoryNamespaceStore(),
		Schedules:  newMemoryScheduleStore(),
		Storage:    newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, pipelineStore, runStore
}

// --- List Runs ---

func TestListRuns_EmptyStore_ReturnsEmptyList(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListRuns_WithData_ReturnsAll(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runStore.runs = []domain.Run{
		{ID: uuid.New(), Status: domain.RunStatusSuccess},
		{ID: uuid.New(), Status: domain.RunStatusFailed},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
}

func TestListRuns_FilterByStatus_ReturnsFiltered(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runStore.runs = []domain.Run{
		{ID: uuid.New(), Status: domain.RunStatusSuccess},
		{ID: uuid.New(), Status: domain.RunStatusFailed},
		{ID: uuid.New(), Status: domain.RunStatusSuccess},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs?status=failed", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(1), body["total"])
}

// --- Get Run ---

func TestGetRun_Exists_ReturnsRun(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusRunning, Trigger: "manual"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "running", body["status"])
	assert.Equal(t, "manual", body["trigger"])
}

func TestGetRun_NotFound_Returns404(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+uuid.New().String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Create Run ---

func TestCreateRun_ValidRequest_Returns202(t *testing.T) {
	srv, pipelineStore, _ := newRunTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerSilver, Name: "orders"},
	}
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","pipeline":"orders","trigger":"manual"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "pending", resp["status"])
	assert.NotEmpty(t, resp["run_id"])
}

func TestCreateRun_MissingPipeline_Returns400(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateRun_PipelineNotFound_Returns404(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","pipeline":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateRun_UppercaseNamespace_Returns400(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"Default","layer":"silver","pipeline":"orders"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestCreateRun_InvalidPipelineName_Returns400(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","pipeline":"My Pipeline"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateRun_InvalidLayer_Returns400(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"platinum","pipeline":"orders"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "layer must be bronze, silver, or gold")
}

func TestCreateRun_DefaultsTriggerToManual(t *testing.T) {
	srv, pipelineStore, _ := newRunTestServer()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "events"},
	}
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze","pipeline":"events"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

// --- Cancel Run ---

func TestCancelRun_PendingRun_ReturnsCancelled(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusPending},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runID.String()+"/cancel", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", resp["status"])
}

func TestCancelRun_RunningRun_ReturnsCancelled(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusRunning},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runID.String()+"/cancel", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCancelRun_CompletedRun_Returns409(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusSuccess},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runID.String()+"/cancel", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestCancelRun_NotFound_Returns404(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+uuid.New().String()+"/cancel", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Run Logs ---

func TestGetRunLogs_JSON_ReturnsLogs(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusSuccess},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "success", body["status"])

	logs := body["logs"].([]interface{})
	assert.Len(t, logs, 2)
}

func TestGetRunLogs_SSE_TerminalRun_ReturnsAllLogsAndCloses(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusSuccess},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "event: log")
	assert.Contains(t, rec.Body.String(), "event: status")
	assert.Contains(t, rec.Body.String(), `"status":"success"`)
}

func TestGetRunLogs_SSE_ActiveRun_ClosesOnClientDisconnect(t *testing.T) {
	srv, _, runStore := newRunTestServer()
	runID := uuid.New()
	runStore.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusRunning},
	}
	router := api.NewRouter(srv)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/logs", http.NoBody)
	req = req.WithContext(ctx)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(rec, req)
		close(done)
	}()

	// Let it stream briefly, then disconnect
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	// Should have sent the initial logs before we cancelled
	assert.Contains(t, rec.Body.String(), "event: log")
}

func TestGetRunLogs_NotFound_Returns404(t *testing.T) {
	srv, _, _ := newRunTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+uuid.New().String()+"/logs", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
