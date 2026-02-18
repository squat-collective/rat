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

// memoryScheduleStore is an in-memory ScheduleStore for tests.
type memoryScheduleStore struct {
	mu        sync.Mutex
	schedules []domain.Schedule
}

func newMemoryScheduleStore() *memoryScheduleStore {
	return &memoryScheduleStore{}
}

func (m *memoryScheduleStore) ListSchedules(_ context.Context) ([]domain.Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]domain.Schedule, len(m.schedules))
	copy(result, m.schedules)
	return result, nil
}

func (m *memoryScheduleStore) GetSchedule(_ context.Context, id string) (*domain.Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil
	}
	for _, s := range m.schedules {
		if s.ID == uid {
			return &s, nil
		}
	}
	return nil, nil
}

func (m *memoryScheduleStore) CreateSchedule(_ context.Context, schedule *domain.Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// P10-27: Check for duplicate schedule (same pipeline + same cron expression)
	// to mirror production Postgres unique constraint behavior.
	for _, s := range m.schedules {
		if s.PipelineID == schedule.PipelineID && s.CronExpr == schedule.CronExpr {
			return fmt.Errorf("schedule for pipeline %s with cron %q already exists", schedule.PipelineID, schedule.CronExpr)
		}
	}

	schedule.ID = uuid.New()
	schedule.CreatedAt = time.Now()
	schedule.UpdatedAt = schedule.CreatedAt
	m.schedules = append(m.schedules, *schedule)
	return nil
}

func (m *memoryScheduleStore) UpdateSchedule(_ context.Context, id string, update api.UpdateScheduleRequest) (*domain.Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil
	}
	for i, s := range m.schedules {
		if s.ID == uid {
			if update.Cron != nil {
				m.schedules[i].CronExpr = *update.Cron
			}
			if update.Enabled != nil {
				m.schedules[i].Enabled = *update.Enabled
			}
			result := m.schedules[i]
			return &result, nil
		}
	}
	return nil, nil
}

func (m *memoryScheduleStore) UpdateScheduleRun(_ context.Context, id, lastRunID string, lastRunAt, nextRunAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid schedule ID")
	}
	for i, s := range m.schedules {
		if s.ID == uid {
			if lastRunID != "" {
				runUID, parseErr := uuid.Parse(lastRunID)
				if parseErr != nil {
					return fmt.Errorf("invalid run ID")
				}
				m.schedules[i].LastRunID = &runUID
			}
			m.schedules[i].LastRunAt = &lastRunAt
			m.schedules[i].NextRunAt = &nextRunAt
			return nil
		}
	}
	return fmt.Errorf("schedule not found")
}

func (m *memoryScheduleStore) DeleteSchedule(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid schedule ID")
	}
	for i, s := range m.schedules {
		if s.ID == uid {
			m.schedules = append(m.schedules[:i], m.schedules[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("schedule not found")
}

// newScheduleTestServer creates a Server with all stores.
func newScheduleTestServer() (*api.Server, *memoryPipelineStore, *memoryScheduleStore) {
	pipelineStore := newMemoryPipelineStore()
	scheduleStore := newMemoryScheduleStore()
	srv := &api.Server{
		Pipelines:  pipelineStore,
		Runs:       newMemoryRunStore(),
		Namespaces: newMemoryNamespaceStore(),
		Schedules:  scheduleStore,
		Storage:    newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, pipelineStore, scheduleStore
}

// --- List Schedules ---

func TestListSchedules_EmptyStore_ReturnsEmptyList(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListSchedules_WithData_ReturnsAll(t *testing.T) {
	srv, _, schedStore := newScheduleTestServer()
	schedStore.schedules = []domain.Schedule{
		{ID: uuid.New(), CronExpr: "0 * * * *", Enabled: true},
		{ID: uuid.New(), CronExpr: "0 0 * * *", Enabled: false},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
}

// --- Get Schedule ---

func TestGetSchedule_Exists_ReturnsSchedule(t *testing.T) {
	srv, _, schedStore := newScheduleTestServer()
	schedID := uuid.New()
	schedStore.schedules = []domain.Schedule{
		{ID: schedID, CronExpr: "0 * * * *", Enabled: true},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/"+schedID.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "0 * * * *", body["cron"])
}

func TestGetSchedule_NotFound_Returns404(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/"+uuid.New().String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Create Schedule ---

func TestCreateSchedule_ValidRequest_Returns201(t *testing.T) {
	srv, pipelineStore, _ := newScheduleTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerSilver, Name: "orders"},
	}
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","pipeline":"orders","cron":"0 * * * *"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "0 * * * *", resp["cron"])
	assert.Equal(t, true, resp["enabled"])
	assert.NotEmpty(t, resp["id"])
}

func TestCreateSchedule_MissingCron_Returns400(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","pipeline":"orders"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateSchedule_UppercaseNamespace_Returns400(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"Default","layer":"silver","pipeline":"orders","cron":"0 * * * *"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestCreateSchedule_InvalidLayer_Returns400(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"platinum","pipeline":"orders","cron":"0 * * * *"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "layer must be bronze, silver, or gold")
}

func TestCreateSchedule_InvalidCronExpression_Returns400(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","pipeline":"orders","cron":"not a cron"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid cron expression")
}

func TestCreateSchedule_PipelineNotFound_Returns404(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"silver","pipeline":"nonexistent","cron":"0 * * * *"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Update Schedule ---

func TestUpdateSchedule_UpdateCron_ReturnsUpdated(t *testing.T) {
	srv, _, schedStore := newScheduleTestServer()
	schedID := uuid.New()
	schedStore.schedules = []domain.Schedule{
		{ID: schedID, CronExpr: "0 * * * *", Enabled: true},
	}
	router := api.NewRouter(srv)

	body := `{"cron":"0 0 * * *"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/schedules/"+schedID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "0 0 * * *", resp["cron"])
}

func TestUpdateSchedule_DisableSchedule_ReturnsUpdated(t *testing.T) {
	srv, _, schedStore := newScheduleTestServer()
	schedID := uuid.New()
	schedStore.schedules = []domain.Schedule{
		{ID: schedID, CronExpr: "0 * * * *", Enabled: true},
	}
	router := api.NewRouter(srv)

	body := `{"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/schedules/"+schedID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, false, resp["enabled"])
}

func TestUpdateSchedule_NotFound_Returns404(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	body := `{"cron":"0 0 * * *"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/schedules/"+uuid.New().String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Delete Schedule ---

func TestDeleteSchedule_Exists_Returns204(t *testing.T) {
	srv, _, schedStore := newScheduleTestServer()
	schedID := uuid.New()
	schedStore.schedules = []domain.Schedule{
		{ID: schedID, CronExpr: "0 * * * *"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/schedules/"+schedID.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteSchedule_NotFound_Returns404(t *testing.T) {
	srv, _, _ := newScheduleTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/schedules/"+uuid.New().String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var body api.APIError
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "schedule not found", body.Error.Message)
	assert.Equal(t, "NOT_FOUND", body.Error.Code)
}
