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

// memoryTriggerStore is an in-memory PipelineTriggerStore for tests.
type memoryTriggerStore struct {
	mu       sync.Mutex
	triggers []domain.PipelineTrigger
}

func newMemoryTriggerStore() *memoryTriggerStore {
	return &memoryTriggerStore{}
}

func (m *memoryTriggerStore) ListTriggers(_ context.Context, pipelineID uuid.UUID) ([]domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.PipelineTrigger
	for _, t := range m.triggers {
		if t.PipelineID == pipelineID {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *memoryTriggerStore) GetTrigger(_ context.Context, triggerID string) (*domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return nil, nil
	}
	for _, t := range m.triggers {
		if t.ID == uid {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *memoryTriggerStore) CreateTrigger(_ context.Context, trigger *domain.PipelineTrigger) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	trigger.ID = uuid.New()
	trigger.CreatedAt = time.Now()
	trigger.UpdatedAt = time.Now()
	m.triggers = append(m.triggers, *trigger)
	return nil
}

func (m *memoryTriggerStore) UpdateTrigger(_ context.Context, triggerID string, update api.UpdateTriggerRequest) (*domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return nil, nil
	}
	for i, t := range m.triggers {
		if t.ID != uid {
			continue
		}
		if update.Config != nil {
			m.triggers[i].Config = *update.Config
		}
		if update.Enabled != nil {
			m.triggers[i].Enabled = *update.Enabled
		}
		if update.CooldownSeconds != nil {
			m.triggers[i].CooldownSeconds = *update.CooldownSeconds
		}
		m.triggers[i].UpdatedAt = time.Now()
		result := m.triggers[i]
		return &result, nil
	}
	return nil, nil
}

func (m *memoryTriggerStore) DeleteTrigger(_ context.Context, triggerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return fmt.Errorf("invalid trigger ID")
	}
	for i, t := range m.triggers {
		if t.ID == uid {
			m.triggers = append(m.triggers[:i], m.triggers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("trigger not found")
}

func (m *memoryTriggerStore) FindTriggersByLandingZone(_ context.Context, namespace, zoneName string) ([]domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.PipelineTrigger
	for _, t := range m.triggers {
		if t.Type != domain.TriggerTypeLandingZoneUpload || !t.Enabled {
			continue
		}
		var cfg struct {
			Namespace string `json:"namespace"`
			ZoneName  string `json:"zone_name"`
		}
		if err := json.Unmarshal(t.Config, &cfg); err != nil {
			continue
		}
		if cfg.Namespace == namespace && cfg.ZoneName == zoneName {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *memoryTriggerStore) FindTriggersByType(_ context.Context, triggerType string) ([]domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.PipelineTrigger
	for _, t := range m.triggers {
		if string(t.Type) == triggerType && t.Enabled {
			result = append(result, t)
		}
	}
	return result, nil
}

// FindTriggerByWebhookToken looks up by token hash (the caller already hashed
// the plaintext token via api.HashWebhookToken).
func (m *memoryTriggerStore) FindTriggerByWebhookToken(_ context.Context, tokenHash string) (*domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.triggers {
		if t.Type != domain.TriggerTypeWebhook || !t.Enabled {
			continue
		}
		var cfg struct {
			TokenHash string `json:"token_hash"`
		}
		if err := json.Unmarshal(t.Config, &cfg); err != nil {
			continue
		}
		if cfg.TokenHash == tokenHash {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *memoryTriggerStore) FindTriggersByPipelineSuccess(_ context.Context, namespace, layer, pipeline string) ([]domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.PipelineTrigger
	for _, t := range m.triggers {
		if t.Type != domain.TriggerTypePipelineSuccess || !t.Enabled {
			continue
		}
		var cfg struct {
			Namespace string `json:"namespace"`
			Layer     string `json:"layer"`
			Pipeline  string `json:"pipeline"`
		}
		if err := json.Unmarshal(t.Config, &cfg); err != nil {
			continue
		}
		if cfg.Namespace == namespace && cfg.Layer == layer && cfg.Pipeline == pipeline {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *memoryTriggerStore) FindTriggersByFilePattern(_ context.Context, namespace, zoneName string) ([]domain.PipelineTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.PipelineTrigger
	for _, t := range m.triggers {
		if t.Type != domain.TriggerTypeFilePattern || !t.Enabled {
			continue
		}
		var cfg struct {
			Namespace string `json:"namespace"`
			ZoneName  string `json:"zone_name"`
		}
		if err := json.Unmarshal(t.Config, &cfg); err != nil {
			continue
		}
		if cfg.Namespace == namespace && cfg.ZoneName == zoneName {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *memoryTriggerStore) UpdateTriggerFired(_ context.Context, triggerID string, runID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return fmt.Errorf("invalid trigger ID")
	}
	for i, t := range m.triggers {
		if t.ID == uid {
			now := time.Now()
			m.triggers[i].LastTriggeredAt = &now
			m.triggers[i].LastRunID = &runID
			return nil
		}
	}
	return fmt.Errorf("trigger not found")
}

// mockExecutor records Submit calls for assertion.
type mockExecutor struct {
	mu      sync.Mutex
	calls   []mockSubmitCall
	failErr error
}

type mockSubmitCall struct {
	RunID      uuid.UUID
	PipelineID uuid.UUID
}

func (m *mockExecutor) Submit(_ context.Context, run *domain.Run, pipeline *domain.Pipeline) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockSubmitCall{RunID: run.ID, PipelineID: pipeline.ID})
	return m.failErr
}

func (m *mockExecutor) Cancel(_ context.Context, runID string) error {
	return nil
}

func (m *mockExecutor) GetLogs(_ context.Context, runID string) ([]api.LogEntry, error) {
	return nil, fmt.Errorf("not available")
}

func (m *mockExecutor) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockExecutor) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return &api.ValidationResult{Valid: true}, nil
}

func (m *mockExecutor) submitCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// newTriggerTestServer creates a Server with stores needed for trigger tests.
func newTriggerTestServer() (*api.Server, *memoryPipelineStore, *memoryTriggerStore) {
	pipelineStore := newMemoryPipelineStore()
	triggerStore := newMemoryTriggerStore()
	srv := &api.Server{
		Pipelines:    pipelineStore,
		Runs:         newMemoryRunStore(),
		Namespaces:   newMemoryNamespaceStore(),
		Schedules:    newMemoryScheduleStore(),
		Storage:      newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
		Triggers:     triggerStore,
	}
	return srv, pipelineStore, triggerStore
}

// --- List Triggers ---

func TestListTriggers_EmptyStore_ReturnsEmptyList(t *testing.T) {
	srv, pipelineStore, _ := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/ingest/triggers", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListTriggers_WithData_ReturnsAll(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{ID: uuid.New(), PipelineID: pipelineID, Type: domain.TriggerTypeLandingZoneUpload, Config: json.RawMessage(`{"namespace":"default","zone_name":"orders"}`), Enabled: true},
		{ID: uuid.New(), PipelineID: pipelineID, Type: domain.TriggerTypeLandingZoneUpload, Config: json.RawMessage(`{"namespace":"default","zone_name":"events"}`), Enabled: false},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/ingest/triggers", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
}

func TestListTriggers_PipelineNotFound_Returns404(t *testing.T) {
	srv, _, _ := newTriggerTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/nonexistent/triggers", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Get Trigger ---

func TestGetTrigger_Exists_ReturnsTrigger(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	triggerID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{ID: triggerID, PipelineID: pipelineID, Type: domain.TriggerTypeLandingZoneUpload, Config: json.RawMessage(`{"namespace":"default","zone_name":"orders"}`), Enabled: true},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/ingest/triggers/"+triggerID.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, triggerID.String(), body["id"])
}

func TestGetTrigger_NotFound_Returns404(t *testing.T) {
	srv, pipelineStore, _ := newTriggerTestServer()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/ingest/triggers/"+uuid.New().String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Create Trigger ---

func TestCreateTrigger_ValidRequest_Returns201(t *testing.T) {
	srv, pipelineStore, _ := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	// Add landing zone to pass validation
	srv.LandingZones.(*memoryLandingZoneStore).zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "orders"}},
	}
	router := api.NewRouter(srv)

	body := `{"type":"landing_zone_upload","config":{"namespace":"default","zone_name":"orders"},"cooldown_seconds":60}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/ingest/triggers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "landing_zone_upload", resp["type"])
	assert.Equal(t, true, resp["enabled"])
	assert.Equal(t, float64(60), resp["cooldown_seconds"])
	assert.NotEmpty(t, resp["id"])
}

func TestCreateTrigger_InvalidType_Returns400(t *testing.T) {
	srv, pipelineStore, _ := newTriggerTestServer()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	router := api.NewRouter(srv)

	body := `{"type":"invalid_type","config":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/ingest/triggers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateTrigger_MissingConfig_Returns400(t *testing.T) {
	srv, pipelineStore, _ := newTriggerTestServer()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	router := api.NewRouter(srv)

	body := `{"type":"landing_zone_upload","config":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/ingest/triggers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateTrigger_PipelineNotFound_Returns404(t *testing.T) {
	srv, _, _ := newTriggerTestServer()
	router := api.NewRouter(srv)

	body := `{"type":"landing_zone_upload","config":{"namespace":"default","zone_name":"orders"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/nonexistent/triggers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateTrigger_LandingZoneNotFound_Returns404(t *testing.T) {
	srv, pipelineStore, _ := newTriggerTestServer()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	router := api.NewRouter(srv)

	body := `{"type":"landing_zone_upload","config":{"namespace":"default","zone_name":"nonexistent"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/ingest/triggers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Update Trigger ---

func TestUpdateTrigger_Enable_ReturnsUpdated(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	triggerID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{ID: triggerID, PipelineID: pipelineID, Type: domain.TriggerTypeLandingZoneUpload, Config: json.RawMessage(`{}`), Enabled: false},
	}
	router := api.NewRouter(srv)

	body := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipelines/default/bronze/ingest/triggers/"+triggerID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["enabled"])
}

func TestUpdateTrigger_Disable_ReturnsUpdated(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	triggerID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{ID: triggerID, PipelineID: pipelineID, Type: domain.TriggerTypeLandingZoneUpload, Config: json.RawMessage(`{}`), Enabled: true},
	}
	router := api.NewRouter(srv)

	body := `{"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipelines/default/bronze/ingest/triggers/"+triggerID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, false, resp["enabled"])
}

func TestUpdateTrigger_Cooldown_ReturnsUpdated(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	triggerID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{ID: triggerID, PipelineID: pipelineID, Type: domain.TriggerTypeLandingZoneUpload, Config: json.RawMessage(`{}`), Enabled: true, CooldownSeconds: 0},
	}
	router := api.NewRouter(srv)

	body := `{"cooldown_seconds":120}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipelines/default/bronze/ingest/triggers/"+triggerID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, float64(120), resp["cooldown_seconds"])
}

func TestUpdateTrigger_NotFound_Returns404(t *testing.T) {
	srv, pipelineStore, _ := newTriggerTestServer()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	router := api.NewRouter(srv)

	body := `{"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipelines/default/bronze/ingest/triggers/"+uuid.New().String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Delete Trigger ---

func TestDeleteTrigger_Exists_Returns204(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	triggerID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{ID: triggerID, PipelineID: pipelineID, Type: domain.TriggerTypeLandingZoneUpload, Config: json.RawMessage(`{}`), Enabled: true},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/bronze/ingest/triggers/"+triggerID.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- Trigger Evaluation ---

func TestEvaluateTriggers_NoMatchingTriggers_NoRuns(t *testing.T) {
	srv, _, _ := newTriggerTestServer()

	// Call evaluateLandingZoneTriggers directly — no triggers → no runs
	srv.HandleEvaluateLandingZoneTriggers(context.Background(), "default", "orders", "")

	runStore := srv.Runs.(*memoryRunStore)
	runStore.mu.Lock()
	defer runStore.mu.Unlock()
	assert.Len(t, runStore.runs, 0)
}

func TestEvaluateTriggers_MatchingTrigger_FiresRun(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{
			ID:              uuid.New(),
			PipelineID:      pipelineID,
			Type:            domain.TriggerTypeLandingZoneUpload,
			Config:          json.RawMessage(`{"namespace":"default","zone_name":"orders"}`),
			Enabled:         true,
			CooldownSeconds: 0,
		},
	}

	exec := &mockExecutor{}
	srv.Executor = exec

	srv.HandleEvaluateLandingZoneTriggers(context.Background(), "default", "orders", "")

	runStore := srv.Runs.(*memoryRunStore)
	runStore.mu.Lock()
	assert.Len(t, runStore.runs, 1)
	assert.Equal(t, "trigger:landing_zone_upload:default/orders", runStore.runs[0].Trigger)
	runStore.mu.Unlock()

	assert.Equal(t, 1, exec.submitCount())
}

func TestEvaluateTriggers_CooldownActive_SkipsRun(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	recentTime := time.Now().Add(-10 * time.Second) // 10s ago
	triggerStore.triggers = []domain.PipelineTrigger{
		{
			ID:              uuid.New(),
			PipelineID:      pipelineID,
			Type:            domain.TriggerTypeLandingZoneUpload,
			Config:          json.RawMessage(`{"namespace":"default","zone_name":"orders"}`),
			Enabled:         true,
			CooldownSeconds: 60, // 60s cooldown
			LastTriggeredAt: &recentTime,
		},
	}

	srv.HandleEvaluateLandingZoneTriggers(context.Background(), "default", "orders", "")

	runStore := srv.Runs.(*memoryRunStore)
	runStore.mu.Lock()
	defer runStore.mu.Unlock()
	assert.Len(t, runStore.runs, 0)
}

func TestEvaluateTriggers_CooldownExpired_FiresRun(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	oldTime := time.Now().Add(-120 * time.Second) // 120s ago, cooldown is 60s
	triggerStore.triggers = []domain.PipelineTrigger{
		{
			ID:              uuid.New(),
			PipelineID:      pipelineID,
			Type:            domain.TriggerTypeLandingZoneUpload,
			Config:          json.RawMessage(`{"namespace":"default","zone_name":"orders"}`),
			Enabled:         true,
			CooldownSeconds: 60,
			LastTriggeredAt: &oldTime,
		},
	}

	srv.HandleEvaluateLandingZoneTriggers(context.Background(), "default", "orders", "")

	runStore := srv.Runs.(*memoryRunStore)
	runStore.mu.Lock()
	defer runStore.mu.Unlock()
	assert.Len(t, runStore.runs, 1)
}

func TestEvaluateTriggers_DisabledTrigger_SkipsRun(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{
			ID:         uuid.New(),
			PipelineID: pipelineID,
			Type:       domain.TriggerTypeLandingZoneUpload,
			Config:     json.RawMessage(`{"namespace":"default","zone_name":"orders"}`),
			Enabled:    false, // disabled
		},
	}

	srv.HandleEvaluateLandingZoneTriggers(context.Background(), "default", "orders", "")

	runStore := srv.Runs.(*memoryRunStore)
	runStore.mu.Lock()
	defer runStore.mu.Unlock()
	assert.Len(t, runStore.runs, 0)
}

func TestEvaluateTriggers_MultiplePipelines_AllFire(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipeline1ID := uuid.New()
	pipeline2ID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipeline1ID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest-a"},
		{ID: pipeline2ID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest-b"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{
			ID:         uuid.New(),
			PipelineID: pipeline1ID,
			Type:       domain.TriggerTypeLandingZoneUpload,
			Config:     json.RawMessage(`{"namespace":"default","zone_name":"orders"}`),
			Enabled:    true,
		},
		{
			ID:         uuid.New(),
			PipelineID: pipeline2ID,
			Type:       domain.TriggerTypeLandingZoneUpload,
			Config:     json.RawMessage(`{"namespace":"default","zone_name":"orders"}`),
			Enabled:    true,
		},
	}

	exec := &mockExecutor{}
	srv.Executor = exec

	srv.HandleEvaluateLandingZoneTriggers(context.Background(), "default", "orders", "")

	runStore := srv.Runs.(*memoryRunStore)
	runStore.mu.Lock()
	defer runStore.mu.Unlock()
	assert.Len(t, runStore.runs, 2)
	assert.Equal(t, 2, exec.submitCount())
}

func TestEvaluateTriggers_ExecutorFailure_StillCreatesRun(t *testing.T) {
	srv, pipelineStore, triggerStore := newTriggerTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "ingest"},
	}
	triggerStore.triggers = []domain.PipelineTrigger{
		{
			ID:         uuid.New(),
			PipelineID: pipelineID,
			Type:       domain.TriggerTypeLandingZoneUpload,
			Config:     json.RawMessage(`{"namespace":"default","zone_name":"orders"}`),
			Enabled:    true,
		},
	}

	exec := &mockExecutor{failErr: fmt.Errorf("executor unavailable")}
	srv.Executor = exec

	srv.HandleEvaluateLandingZoneTriggers(context.Background(), "default", "orders", "")

	runStore := srv.Runs.(*memoryRunStore)
	runStore.mu.Lock()
	defer runStore.mu.Unlock()
	assert.Len(t, runStore.runs, 1) // Run was still created even though executor failed
}
