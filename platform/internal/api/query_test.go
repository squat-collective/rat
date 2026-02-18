package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newQueryTestServer() (*api.Server, *memoryQueryStore) {
	qStore := newMemoryQueryStore()
	srv := &api.Server{
		Pipelines:  newMemoryPipelineStore(),
		Runs:       newMemoryRunStore(),
		Namespaces: newMemoryNamespaceStore(),
		Schedules:  newMemoryScheduleStore(),
		Storage:    newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        qStore,
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, qStore
}

// --- Execute Query ---

func TestExecuteQuery_ValidSQL_ReturnsResults(t *testing.T) {
	srv, _ := newQueryTestServer()
	router := api.NewRouter(srv)

	body := `{"sql":"SELECT 1 as result","namespace":"default"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result api.QueryResult
	err := json.NewDecoder(rec.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalRows)
	assert.Len(t, result.Columns, 1)
	assert.Equal(t, "result", result.Columns[0].Name)
}

func TestExecuteQuery_MissingSQL_Returns400(t *testing.T) {
	srv, _ := newQueryTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestExecuteQuery_TooLong_Returns400(t *testing.T) {
	srv, _ := newQueryTestServer()
	router := api.NewRouter(srv)

	// Build a query that exceeds 100KB (100,001 spaces + "SELECT 1" = 100,009 chars)
	longSQL := "SELECT 1" + strings.Repeat(" ", 100_001)
	bodyMap := map[string]interface{}{"sql": longSQL, "namespace": "default"}
	bodyBytes, _ := json.Marshal(bodyMap)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "query too long")
}

func TestExecuteQuery_DefaultsLimitTo1000(t *testing.T) {
	srv, _ := newQueryTestServer()
	router := api.NewRouter(srv)

	body := `{"sql":"SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- List Tables ---

func TestListTables_EmptyStore_ReturnsEmptyList(t *testing.T) {
	srv, _ := newQueryTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tables", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListTables_WithData_ReturnsAll(t *testing.T) {
	srv, qStore := newQueryTestServer()
	qStore.tables = []api.TableInfo{
		{Namespace: "default", Layer: "silver", Name: "orders", RowCount: 1000},
		{Namespace: "default", Layer: "gold", Name: "revenue", RowCount: 500},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tables", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
}

func TestListTables_FilterByLayer_ReturnsFiltered(t *testing.T) {
	srv, qStore := newQueryTestServer()
	qStore.tables = []api.TableInfo{
		{Namespace: "default", Layer: "silver", Name: "orders"},
		{Namespace: "default", Layer: "gold", Name: "revenue"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tables?layer=gold", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(1), body["total"])
}

// --- Get Schema (bulk) ---

func TestGetSchema_ReturnsAllTablesWithColumns(t *testing.T) {
	srv, qStore := newQueryTestServer()
	qStore.tables = []api.TableInfo{
		{Namespace: "default", Layer: "bronze", Name: "orders", RowCount: 100},
		{Namespace: "default", Layer: "silver", Name: "users", RowCount: 50},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/schema", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Tables []api.SchemaEntry `json:"tables"`
	}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Len(t, body.Tables, 2)

	// Each table should have columns from GetTable (memoryQueryStore returns [{id, VARCHAR}])
	for _, entry := range body.Tables {
		assert.NotEmpty(t, entry.Columns, "table %s should have columns", entry.Name)
		assert.Equal(t, "id", entry.Columns[0].Name)
	}
}

func TestGetSchema_EmptyStore_ReturnsEmptyList(t *testing.T) {
	srv, _ := newQueryTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/schema", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Tables []api.SchemaEntry `json:"tables"`
	}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Empty(t, body.Tables)
}

// --- Get Table ---

func TestGetTable_Exists_ReturnsDetail(t *testing.T) {
	srv, qStore := newQueryTestServer()
	qStore.tables = []api.TableInfo{
		{Namespace: "default", Layer: "silver", Name: "orders", RowCount: 1000},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tables/default/silver/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "orders", body["name"])
	assert.NotNil(t, body["columns"])
}

func TestGetTable_NotFound_Returns404(t *testing.T) {
	srv, _ := newQueryTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tables/default/silver/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Preview Table ---

func TestPreviewTable_Exists_ReturnsRows(t *testing.T) {
	srv, qStore := newQueryTestServer()
	qStore.tables = []api.TableInfo{
		{Namespace: "default", Layer: "silver", Name: "orders"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tables/default/silver/orders/preview", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result api.QueryResult
	err := json.NewDecoder(rec.Body).Decode(&result)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.TotalRows, 0)
}
