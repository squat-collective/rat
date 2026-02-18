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

// memoryVersionStore is an in-memory VersionStore for tests.
type memoryVersionStore struct {
	mu       sync.Mutex
	versions []domain.PipelineVersion
}

func newMemoryVersionStore() *memoryVersionStore {
	return &memoryVersionStore{}
}

func (m *memoryVersionStore) ListVersions(_ context.Context, pipelineID uuid.UUID) ([]domain.PipelineVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.PipelineVersion
	for _, v := range m.versions {
		if v.PipelineID == pipelineID {
			result = append(result, v)
		}
	}
	// Sort by version number descending
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].VersionNumber > result[i].VersionNumber {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result, nil
}

func (m *memoryVersionStore) GetVersion(_ context.Context, pipelineID uuid.UUID, versionNumber int) (*domain.PipelineVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, v := range m.versions {
		if v.PipelineID == pipelineID && v.VersionNumber == versionNumber {
			return &v, nil
		}
	}
	return nil, nil
}

func (m *memoryVersionStore) CreateVersion(_ context.Context, v *domain.PipelineVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.versions {
		if existing.PipelineID == v.PipelineID && existing.VersionNumber == v.VersionNumber {
			return fmt.Errorf("version %d already exists for pipeline %s", v.VersionNumber, v.PipelineID)
		}
	}

	v.ID = uuid.New()
	v.CreatedAt = time.Now()
	m.versions = append(m.versions, *v)
	return nil
}

func (m *memoryVersionStore) PruneVersions(_ context.Context, pipelineID uuid.UUID, keepCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Gather versions for this pipeline, sorted descending
	var pipelineVersions []int
	for _, v := range m.versions {
		if v.PipelineID == pipelineID {
			pipelineVersions = append(pipelineVersions, v.VersionNumber)
		}
	}
	// Sort descending
	for i := 0; i < len(pipelineVersions); i++ {
		for j := i + 1; j < len(pipelineVersions); j++ {
			if pipelineVersions[j] > pipelineVersions[i] {
				pipelineVersions[i], pipelineVersions[j] = pipelineVersions[j], pipelineVersions[i]
			}
		}
	}

	// Keep only the top keepCount
	toDelete := make(map[int]bool)
	for i, vn := range pipelineVersions {
		if i >= keepCount {
			toDelete[vn] = true
		}
	}

	var remaining []domain.PipelineVersion
	for _, v := range m.versions {
		if v.PipelineID == pipelineID && toDelete[v.VersionNumber] {
			continue
		}
		remaining = append(remaining, v)
	}
	m.versions = remaining
	return nil
}

func (m *memoryVersionStore) LatestVersionNumber(_ context.Context, pipelineID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	maxVersion := 0
	for _, v := range m.versions {
		if v.PipelineID == pipelineID && v.VersionNumber > maxVersion {
			maxVersion = v.VersionNumber
		}
	}
	return maxVersion, nil
}

// newVersionTestServer creates a Server wired with version store.
func newVersionTestServer() (*api.Server, *memoryPipelineStore, *memoryVersionStore) {
	pipelineStore := newMemoryPipelineStore()
	versionStore := newMemoryVersionStore()
	srv := &api.Server{
		Pipelines:    pipelineStore,
		Versions:     versionStore,
		Runs:         newMemoryRunStore(),
		Namespaces:   newMemoryNamespaceStore(),
		Schedules:    newMemoryScheduleStore(),
		Storage:      newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, pipelineStore, versionStore
}

// --- List Versions ---

func TestListVersions_ReturnsVersionHistory(t *testing.T) {
	srv, pipelineStore, versionStore := newVersionTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql"},
	}
	versionStore.versions = []domain.PipelineVersion{
		{ID: uuid.New(), PipelineID: pipelineID, VersionNumber: 1, Message: "Initial", CreatedAt: time.Now()},
		{ID: uuid.New(), PipelineID: pipelineID, VersionNumber: 2, Message: "Fixed join", CreatedAt: time.Now()},
	}

	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders/versions", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])

	versions := body["versions"].([]interface{})
	// Should be sorted descending
	v1 := versions[0].(map[string]interface{})
	assert.Equal(t, float64(2), v1["version_number"])
	assert.Equal(t, "Fixed join", v1["message"])
}

// --- Get Version ---

func TestGetVersion_ReturnsSpecificVersion(t *testing.T) {
	srv, pipelineStore, versionStore := newVersionTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql"},
	}
	versionStore.versions = []domain.PipelineVersion{
		{
			ID: uuid.New(), PipelineID: pipelineID, VersionNumber: 1, Message: "Initial",
			PublishedVersions: map[string]string{"file.sql": "v1-id"},
			CreatedAt:         time.Now(),
		},
	}

	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders/versions/1", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(1), body["version_number"])
	assert.Equal(t, "Initial", body["message"])
	pv := body["published_versions"].(map[string]interface{})
	assert.Equal(t, "v1-id", pv["file.sql"])
}

// --- Rollback ---

func TestRollback_CreatesNewVersionWithOldSnapshot(t *testing.T) {
	srv, pipelineStore, versionStore := newVersionTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "events", Type: "sql", MaxVersions: 50},
	}
	versionStore.versions = []domain.PipelineVersion{
		{
			ID: uuid.New(), PipelineID: pipelineID, VersionNumber: 1,
			Message:           "v1",
			PublishedVersions: map[string]string{"pipeline.sql": "version-a"},
			CreatedAt:         time.Now(),
		},
		{
			ID: uuid.New(), PipelineID: pipelineID, VersionNumber: 2,
			Message:           "v2",
			PublishedVersions: map[string]string{"pipeline.sql": "version-b"},
			CreatedAt:         time.Now(),
		},
	}

	router := api.NewRouter(srv)

	body := `{"version": 1, "message": "Rolling back to v1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/events/rollback", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "rolled_back", resp["status"])
	assert.Equal(t, float64(1), resp["from_version"])
	assert.Equal(t, float64(3), resp["new_version"])

	// Verify v3 was created with v1's snapshot
	v3, _ := versionStore.GetVersion(context.Background(), pipelineID, 3)
	require.NotNil(t, v3)
	assert.Equal(t, "version-a", v3.PublishedVersions["pipeline.sql"])
	assert.Equal(t, "Rolling back to v1", v3.Message)

	// Verify pipeline's published_versions was updated
	p, _ := pipelineStore.GetPipeline(context.Background(), "default", "bronze", "events")
	require.NotNil(t, p)
	assert.Equal(t, "version-a", p.PublishedVersions["pipeline.sql"])
}

func TestRollback_NonexistentVersion_Returns404(t *testing.T) {
	srv, pipelineStore, _ := newVersionTestServer()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "events", Type: "sql"},
	}

	router := api.NewRouter(srv)

	body := `{"version": 99}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/events/rollback", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Publish with Version ---

func TestPublish_WithMessage_CreatesVersion(t *testing.T) {
	srv, pipelineStore, versionStore := newVersionTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql", MaxVersions: 50},
	}

	// Seed storage with pipeline files
	storageStore := srv.Storage.(*memoryStorageStore)
	storageStore.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("SELECT 1")

	router := api.NewRouter(srv)

	body := `{"message": "Fixed join condition"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/publish", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "published", resp["status"])
	assert.Equal(t, float64(1), resp["version"])
	assert.Equal(t, "Fixed join condition", resp["message"])

	// Verify version was created
	versions, _ := versionStore.ListVersions(context.Background(), pipelineID)
	require.Len(t, versions, 1)
	assert.Equal(t, 1, versions[0].VersionNumber)
	assert.Equal(t, "Fixed join condition", versions[0].Message)
}

func TestPublish_PrunesOldVersions(t *testing.T) {
	srv, pipelineStore, versionStore := newVersionTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "events", Type: "sql", MaxVersions: 3},
	}

	// Pre-seed 3 versions
	for i := 1; i <= 3; i++ {
		versionStore.versions = append(versionStore.versions, domain.PipelineVersion{
			ID: uuid.New(), PipelineID: pipelineID, VersionNumber: i,
			Message:           fmt.Sprintf("v%d", i),
			PublishedVersions: map[string]string{"pipeline.sql": fmt.Sprintf("vid-%d", i)},
			CreatedAt:         time.Now(),
		})
	}

	// Seed storage
	storageStore := srv.Storage.(*memoryStorageStore)
	storageStore.files["default/pipelines/bronze/events/pipeline.sql"] = []byte("SELECT 1")

	router := api.NewRouter(srv)

	// Publish a 4th version — should prune v1
	body := `{"message": "v4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/events/publish", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Should have 3 versions (v2, v3, v4) — v1 pruned
	versions, _ := versionStore.ListVersions(context.Background(), pipelineID)
	assert.Len(t, versions, 3)
	assert.Equal(t, 4, versions[0].VersionNumber)
	assert.Equal(t, 3, versions[1].VersionNumber)
	assert.Equal(t, 2, versions[2].VersionNumber)
}

// --- Publisher (transactional) tests ---

// memoryPublisher is an in-memory PipelinePublisher that delegates to
// memoryPipelineStore and memoryVersionStore, simulating the transactional
// behavior for handler-level tests.
type memoryPublisher struct {
	pipelines *memoryPipelineStore
	versions  *memoryVersionStore
}

func (p *memoryPublisher) PublishPipelineTx(ctx context.Context, ns, layer, name string, versions map[string]string, pv *domain.PipelineVersion, keepCount int) error {
	if err := p.pipelines.PublishPipeline(ctx, ns, layer, name, versions); err != nil {
		return err
	}
	if err := p.versions.CreateVersion(ctx, pv); err != nil {
		return err
	}
	return p.versions.PruneVersions(ctx, pv.PipelineID, keepCount)
}

func (p *memoryPublisher) RollbackPipelineTx(ctx context.Context, ns, layer, name string, versions map[string]string, pv *domain.PipelineVersion, keepCount int) error {
	if err := p.versions.CreateVersion(ctx, pv); err != nil {
		return err
	}
	if err := p.pipelines.PublishPipeline(ctx, ns, layer, name, versions); err != nil {
		return err
	}
	return p.versions.PruneVersions(ctx, pv.PipelineID, keepCount)
}

// newPublisherTestServer creates a Server wired with a PipelinePublisher.
func newPublisherTestServer() (*api.Server, *memoryPipelineStore, *memoryVersionStore) {
	pipelineStore := newMemoryPipelineStore()
	versionStore := newMemoryVersionStore()
	publisher := &memoryPublisher{pipelines: pipelineStore, versions: versionStore}
	srv := &api.Server{
		Pipelines:    pipelineStore,
		Versions:     versionStore,
		Publisher:    publisher,
		Runs:         newMemoryRunStore(),
		Namespaces:   newMemoryNamespaceStore(),
		Schedules:    newMemoryScheduleStore(),
		Storage:      newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, pipelineStore, versionStore
}

func TestPublishTx_UsesPublisherWhenAvailable(t *testing.T) {
	srv, pipelineStore, versionStore := newPublisherTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql", MaxVersions: 50},
	}

	storageStore := srv.Storage.(*memoryStorageStore)
	storageStore.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("SELECT 1")

	router := api.NewRouter(srv)

	body := `{"message": "Transactional publish"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/publish", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "published", resp["status"])
	assert.Equal(t, float64(1), resp["version"])

	// Verify version was created via the publisher
	versions, _ := versionStore.ListVersions(context.Background(), pipelineID)
	require.Len(t, versions, 1)
	assert.Equal(t, "Transactional publish", versions[0].Message)

	// Verify pipeline was published
	p, _ := pipelineStore.GetPipeline(context.Background(), "default", "silver", "orders")
	require.NotNil(t, p)
	assert.NotNil(t, p.PublishedAt)
}

func TestRollbackTx_UsesPublisherWhenAvailable(t *testing.T) {
	srv, pipelineStore, versionStore := newPublisherTestServer()
	pipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: pipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "events", Type: "sql", MaxVersions: 50},
	}
	versionStore.versions = []domain.PipelineVersion{
		{
			ID: uuid.New(), PipelineID: pipelineID, VersionNumber: 1,
			Message:           "v1",
			PublishedVersions: map[string]string{"pipeline.sql": "version-a"},
			CreatedAt:         time.Now(),
		},
		{
			ID: uuid.New(), PipelineID: pipelineID, VersionNumber: 2,
			Message:           "v2",
			PublishedVersions: map[string]string{"pipeline.sql": "version-b"},
			CreatedAt:         time.Now(),
		},
	}

	router := api.NewRouter(srv)

	body := `{"version": 1, "message": "Tx rollback to v1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/bronze/events/rollback", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "rolled_back", resp["status"])
	assert.Equal(t, float64(3), resp["new_version"])

	// Verify v3 was created with v1's snapshot via the publisher
	v3, _ := versionStore.GetVersion(context.Background(), pipelineID, 3)
	require.NotNil(t, v3)
	assert.Equal(t, "version-a", v3.PublishedVersions["pipeline.sql"])
	assert.Equal(t, "Tx rollback to v1", v3.Message)

	// Verify pipeline's published_versions was updated
	p, _ := pipelineStore.GetPipeline(context.Background(), "default", "bronze", "events")
	require.NotNil(t, p)
	assert.Equal(t, "version-a", p.PublishedVersions["pipeline.sql"])
}

// --- Bug Fix Validation ---

func TestGetPipeline_ReturnsVersioningFields(t *testing.T) {
	srv, store := newTestServer()
	now := time.Now()
	store.pipelines = []domain.Pipeline{
		{
			Namespace:         "default",
			Layer:             domain.LayerSilver,
			Name:              "orders",
			Type:              "sql",
			DraftDirty:        true,
			PublishedAt:       &now,
			PublishedVersions: map[string]string{"pipeline.sql": "v1-id"},
			MaxVersions:       25,
		},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)

	// Verify versioning fields are present in the response
	assert.Equal(t, true, body["draft_dirty"])
	assert.NotNil(t, body["published_at"])
	pv := body["published_versions"].(map[string]interface{})
	assert.Equal(t, "v1-id", pv["pipeline.sql"])
	assert.Equal(t, float64(25), body["max_versions"])
}
