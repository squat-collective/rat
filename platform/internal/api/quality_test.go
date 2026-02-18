package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newQualityTestServer() (*api.Server, *memoryQualityStore) {
	qStore := newMemoryQualityStore()
	srv := &api.Server{
		Pipelines:  newMemoryPipelineStore(),
		Runs:       newMemoryRunStore(),
		Namespaces: newMemoryNamespaceStore(),
		Schedules:  newMemoryScheduleStore(),
		Storage:    newMemoryStorageStore(),
		Quality:      qStore,
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, qStore
}

// --- List Quality Tests ---

func TestListQualityTests_Empty_ReturnsEmptyList(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders/tests", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListQualityTests_WithData_ReturnsAll(t *testing.T) {
	srv, qStore := newQualityTestServer()
	qStore.tests["default/silver/orders"] = []api.QualityTest{
		{Name: "no_null_ids", SQL: "SELECT COUNT(*) FROM {{ this }} WHERE id IS NULL", Severity: "error"},
		{Name: "positive_amounts", SQL: "SELECT COUNT(*) FROM {{ this }} WHERE amount < 0", Severity: "warn"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders/tests", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
}

func TestListQualityTests_AnnotatesPublished(t *testing.T) {
	srv, qStore := newQualityTestServer()
	// Add a pipeline with published_versions containing one test
	pStore := srv.Pipelines.(*memoryPipelineStore)
	pStore.pipelines = append(pStore.pipelines, domain.Pipeline{
		Namespace: "default",
		Layer:     "silver",
		Name:      "orders",
		PublishedVersions: map[string]string{
			"default/pipelines/silver/orders/pipeline.sql":                    "v1",
			"default/pipelines/silver/orders/tests/quality/no_null_ids.sql":   "v2",
		},
	})
	qStore.tests["default/silver/orders"] = []api.QualityTest{
		{Name: "no_null_ids", SQL: "SELECT 1", Severity: "error"},
		{Name: "positive_amounts", SQL: "SELECT 1", Severity: "warn"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders/tests", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Tests []struct {
			Name      string `json:"name"`
			Published bool   `json:"published"`
		} `json:"tests"`
	}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	require.Len(t, body.Tests, 2)
	// no_null_ids is in published_versions → published
	assert.True(t, body.Tests[0].Published, "no_null_ids should be published")
	// positive_amounts is NOT in published_versions → draft
	assert.False(t, body.Tests[1].Published, "positive_amounts should be draft")
}

func TestListQualityTests_NeverPublished_AllDraft(t *testing.T) {
	srv, qStore := newQualityTestServer()
	// Pipeline exists but has nil PublishedVersions (never published)
	pStore := srv.Pipelines.(*memoryPipelineStore)
	pStore.pipelines = append(pStore.pipelines, domain.Pipeline{
		Namespace:         "default",
		Layer:             "silver",
		Name:              "orders",
		PublishedVersions: nil,
	})
	qStore.tests["default/silver/orders"] = []api.QualityTest{
		{Name: "test1", SQL: "SELECT 1", Severity: "error"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/silver/orders/tests", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Tests []struct {
			Name      string `json:"name"`
			Published bool   `json:"published"`
		} `json:"tests"`
	}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	require.Len(t, body.Tests, 1)
	assert.False(t, body.Tests[0].Published, "test1 should be draft when never published")
}

// --- Create Quality Test ---

func TestCreateQualityTest_ValidRequest_Returns201(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"no_null_ids","sql":"SELECT COUNT(*) FROM {{ this }} WHERE id IS NULL","severity":"error"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "no_null_ids", resp["name"])
	assert.Contains(t, resp["path"], "tests/quality/no_null_ids.sql")
}

func TestCreateQualityTest_MissingSQL_Returns400(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"test1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateQualityTest_Duplicate_Returns409(t *testing.T) {
	srv, qStore := newQualityTestServer()
	qStore.tests["default/silver/orders"] = []api.QualityTest{
		{Name: "no_null_ids", SQL: "SELECT 1"},
	}
	router := api.NewRouter(srv)

	body := `{"name":"no_null_ids","sql":"SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestCreateQualityTest_InvalidTestName_Returns400(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"Bad Name!","sql":"SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestCreateQualityTest_UppercaseTestName_Returns400(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"NoNullIds","sql":"SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateQualityTest_DefaultsSeverityToError(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"test1","sql":"SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "error", resp["severity"].(string))
}

// --- Delete Quality Test ---

func TestDeleteQualityTest_Exists_Returns204(t *testing.T) {
	srv, qStore := newQualityTestServer()
	qStore.tests["default/silver/orders"] = []api.QualityTest{
		{Name: "no_null_ids"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/silver/orders/tests/no_null_ids", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteQualityTest_NotFound_Returns500(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/silver/orders/tests/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// --- Run Quality Tests ---

func TestRunQualityTests_WithTests_ReturnsResults(t *testing.T) {
	srv, qStore := newQualityTestServer()
	qStore.tests["default/silver/orders"] = []api.QualityTest{
		{Name: "no_null_ids", SQL: "SELECT 1", Severity: "error"},
		{Name: "positive_amounts", SQL: "SELECT 1", Severity: "warn"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests/run", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
	assert.Equal(t, float64(2), body["passed"])
	assert.Equal(t, float64(0), body["failed"])
}

func TestRunQualityTests_NoTests_ReturnsEmpty(t *testing.T) {
	srv, _ := newQualityTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/default/silver/orders/tests/run", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}
