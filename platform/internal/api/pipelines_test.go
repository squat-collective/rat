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

// memoryPipelineStore is an in-memory PipelineStore for tests.
type memoryPipelineStore struct {
	mu              sync.Mutex
	pipelines       []domain.Pipeline
	retentionConfig map[uuid.UUID]json.RawMessage
}

func newMemoryPipelineStore() *memoryPipelineStore {
	return &memoryPipelineStore{
		retentionConfig: make(map[uuid.UUID]json.RawMessage),
	}
}

func (m *memoryPipelineStore) filteredPipelines(filter api.PipelineFilter) []domain.Pipeline {
	var result []domain.Pipeline
	for _, p := range m.pipelines {
		if filter.Namespace != "" && p.Namespace != filter.Namespace {
			continue
		}
		if filter.Layer != "" && string(p.Layer) != filter.Layer {
			continue
		}
		result = append(result, p)
	}
	return result
}

func (m *memoryPipelineStore) ListPipelines(_ context.Context, filter api.PipelineFilter) ([]domain.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := m.filteredPipelines(filter)
	if filter.Limit > 0 {
		if filter.Offset >= len(result) {
			return []domain.Pipeline{}, nil
		}
		end := filter.Offset + filter.Limit
		if end > len(result) {
			end = len(result)
		}
		result = result[filter.Offset:end]
	}
	return result, nil
}

func (m *memoryPipelineStore) CountPipelines(_ context.Context, filter api.PipelineFilter) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.filteredPipelines(filter)), nil
}

func (m *memoryPipelineStore) GetPipeline(_ context.Context, namespace, layer, name string) (*domain.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.pipelines {
		if p.Namespace == namespace && string(p.Layer) == layer && p.Name == name {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *memoryPipelineStore) GetPipelineByID(_ context.Context, id string) (*domain.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil
	}
	for _, p := range m.pipelines {
		if p.ID == uid {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *memoryPipelineStore) CreatePipeline(_ context.Context, p *domain.Pipeline) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.pipelines {
		if existing.Namespace == p.Namespace && existing.Layer == p.Layer && existing.Name == p.Name {
			return fmt.Errorf("pipeline %s/%s/%s: %w", p.Namespace, p.Layer, p.Name, domain.ErrAlreadyExists)
		}
	}
	m.pipelines = append(m.pipelines, *p)
	return nil
}

func (m *memoryPipelineStore) UpdatePipeline(_ context.Context, namespace, layer, name string, update api.UpdatePipelineRequest) (*domain.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, p := range m.pipelines {
		if p.Namespace == namespace && string(p.Layer) == layer && p.Name == name {
			if update.Description != nil {
				m.pipelines[i].Description = *update.Description
			}
			if update.Type != nil {
				m.pipelines[i].Type = *update.Type
			}
			result := m.pipelines[i]
			return &result, nil
		}
	}
	return nil, nil
}

func (m *memoryPipelineStore) DeletePipeline(_ context.Context, namespace, layer, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, p := range m.pipelines {
		if p.Namespace == namespace && string(p.Layer) == layer && p.Name == name {
			m.pipelines = append(m.pipelines[:i], m.pipelines[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("pipeline not found")
}

func (m *memoryPipelineStore) SetDraftDirty(_ context.Context, namespace, layer, name string, dirty bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, p := range m.pipelines {
		if p.Namespace == namespace && string(p.Layer) == layer && p.Name == name {
			m.pipelines[i].DraftDirty = dirty
			return nil
		}
	}
	return nil // no-op if not found
}

func (m *memoryPipelineStore) UpdatePipelineRetention(_ context.Context, pipelineID uuid.UUID, config json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.retentionConfig[pipelineID] = config
	return nil
}

func (m *memoryPipelineStore) ListSoftDeletedPipelines(_ context.Context, _ time.Time) ([]domain.Pipeline, error) {
	return nil, nil
}

func (m *memoryPipelineStore) HardDeletePipeline(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *memoryPipelineStore) PublishPipeline(_ context.Context, namespace, layer, name string, versions map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, p := range m.pipelines {
		if p.Namespace != namespace || string(p.Layer) != layer || p.Name != name {
			continue
		}
		now := p.UpdatedAt // reuse for simplicity
		m.pipelines[i].PublishedAt = &now
		m.pipelines[i].PublishedVersions = versions
		m.pipelines[i].DraftDirty = false
		return nil
	}
	return fmt.Errorf("pipeline not found")
}

// newTestServer creates a Server wired with an in-memory store.
func newTestServer() (*api.Server, *memoryPipelineStore) {
	store := newMemoryPipelineStore()
	srv := &api.Server{
		Pipelines:    store,
		Runs:         newMemoryRunStore(),
		Namespaces:   newMemoryNamespaceStore(),
		Schedules:    newMemoryScheduleStore(),
		Storage:      newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, store
}

// --- List Pipelines ---

func TestListPipelines_EmptyStore_ReturnsEmptyList(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListPipelines_WithData_ReturnsAll(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "orders"},
		{Namespace: "default", Layer: domain.LayerSilver, Name: "customers"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
}

func TestListPipelines_FilterByNamespace_ReturnsFiltered(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "analytics", Layer: domain.LayerBronze, Name: "orders"},
		{Namespace: "marketing", Layer: domain.LayerBronze, Name: "campaigns"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines?namespace=analytics", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(1), body["total"])
}

// --- Get Pipeline ---

func TestGetPipeline_Exists_ReturnsPipeline(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "orders", body["name"])
	assert.Equal(t, "silver", body["layer"])
}

func TestGetPipeline_NotFound_Returns404(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Create Pipeline ---

func TestCreatePipeline_ValidRequest_Returns201(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze","name":"orders","type":"sql"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "orders", resp["name"])
	assert.Equal(t, "default/pipelines/bronze/orders/", resp["s3_path"])
}

func TestCreatePipeline_MissingName_Returns400(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreatePipeline_InvalidLayer_Returns400(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"platinum","name":"orders"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreatePipeline_UppercaseName_Returns400(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze","name":"MyPipeline"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestCreatePipeline_NameWithSpaces_Returns400(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze","name":"my pipeline"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreatePipeline_Duplicate_Returns409(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "orders"},
	}
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze","name":"orders"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestCreatePipeline_DefaultsToSQL(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","name":"products"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

// --- Update Pipeline ---

func TestUpdatePipeline_UpdateDescription_ReturnsUpdated(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "orders", Type: "sql", Description: "old desc"},
	}
	router := api.NewRouter(srv)

	body := `{"description":"new description"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipelines/default/bronze/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "new description", resp["description"])
	assert.Equal(t, "orders", resp["name"])
}

func TestUpdatePipeline_NotFound_Returns404(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	body := `{"description":"test"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipelines/default/bronze/nonexistent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Delete Pipeline ---

func TestDeletePipeline_Exists_Returns204(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "orders"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/bronze/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeletePipeline_NotFound_Returns404(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/bronze/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Publish Pipeline ---

func TestHandlePublishPipeline_SnapshotsVersionIDs(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql"},
	}
	// Seed storage with pipeline files
	storageStore := srv.Storage.(*memoryStorageStore)
	storageStore.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("SELECT 1")
	storageStore.files["default/pipelines/silver/orders/config.yaml"] = []byte("key: val")

	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/publish", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "published", body["status"])
	versions := body["versions"].(map[string]interface{})
	assert.Contains(t, versions, "default/pipelines/silver/orders/pipeline.sql")
	assert.Contains(t, versions, "default/pipelines/silver/orders/config.yaml")
}

func TestHandlePublishPipeline_ClearsDraftDirty(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "events", Type: "sql", DraftDirty: true},
	}
	storageStore := srv.Storage.(*memoryStorageStore)
	storageStore.files["default/pipelines/bronze/events/pipeline.sql"] = []byte("SELECT 1")

	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/events/publish", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify draft_dirty was cleared
	p, _ := store.GetPipeline(context.Background(), "default", "bronze", "events")
	require.NotNil(t, p)
	assert.False(t, p.DraftDirty)
	assert.NotNil(t, p.PublishedAt)
}

func TestHandlePublishPipeline_NotFound_Returns404(t *testing.T) {
	srv, _ := newTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/nonexistent/publish", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- publishMockExecutor for validation tests ---

type publishMockExecutor struct {
	validateResult *api.ValidationResult
	validateErr    error
}

func (m *publishMockExecutor) Submit(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error {
	return nil
}
func (m *publishMockExecutor) Cancel(_ context.Context, _ string) error { return nil }
func (m *publishMockExecutor) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}
func (m *publishMockExecutor) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	return nil, nil
}
func (m *publishMockExecutor) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return m.validateResult, m.validateErr
}

func TestPublishPipeline_ValidationErrors_Returns422(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql"},
	}
	srv.Executor = &publishMockExecutor{
		validateResult: &api.ValidationResult{
			Valid: false,
			Files: []api.FileValidation{
				{
					Path:   "default/pipelines/silver/orders/pipeline.sql",
					Valid:  false,
					Errors: []string{"Jinja syntax error: unexpected '}'"},
				},
			},
		},
	}

	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/publish", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "template validation failed", body["error"])
	assert.NotNil(t, body["validation"])
}

func TestPublishPipeline_ValidationWarnings_StillPublishes(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql"},
	}
	storageStore := srv.Storage.(*memoryStorageStore)
	storageStore.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("SELECT 1")

	srv.Executor = &publishMockExecutor{
		validateResult: &api.ValidationResult{
			Valid: true,
			Files: []api.FileValidation{
				{
					Path:     "default/pipelines/silver/orders/pipeline.sql",
					Valid:    true,
					Warnings: []string{"Bare function call outside Jinja delimiters"},
				},
			},
		},
	}

	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/publish", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "published", body["status"])
}
