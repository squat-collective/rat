package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMetaTestServer() (*api.Server, *memoryStorageStore) {
	storageStore := newMemoryStorageStore()
	srv := &api.Server{
		Pipelines:  newMemoryPipelineStore(),
		Runs:       newMemoryRunStore(),
		Namespaces: newMemoryNamespaceStore(),
		Schedules:  newMemoryScheduleStore(),
		Storage:    storageStore,
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, storageStore
}

// --- Pipeline Metadata ---

func TestGetPipelineMeta_Exists_ReturnsContent(t *testing.T) {
	srv, store := newMetaTestServer()
	store.files["default/pipelines/silver/orders/pipeline.meta.yaml"] = []byte("runs:\n  - run_id: abc123")
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/default/pipeline/silver/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Contains(t, body["content"], "run_id: abc123")
}

func TestGetPipelineMeta_NotFound_Returns404(t *testing.T) {
	srv, _ := newMetaTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/default/pipeline/silver/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Quality Metadata ---

func TestGetQualityMeta_Exists_ReturnsContent(t *testing.T) {
	srv, store := newMetaTestServer()
	store.files["default/pipelines/silver/orders/tests/quality.meta.yaml"] = []byte("results:\n  - name: no_null_ids")
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/default/quality/silver/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Contains(t, body["content"], "no_null_ids")
}

func TestGetQualityMeta_NotFound_Returns404(t *testing.T) {
	srv, _ := newMetaTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/default/quality/silver/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
