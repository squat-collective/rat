package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLandingTestServer creates a Server wired for landing zone tests.
func newLandingTestServer() (*api.Server, *memoryLandingZoneStore) {
	lzStore := newMemoryLandingZoneStore()
	srv := &api.Server{
		Pipelines:    newMemoryPipelineStore(),
		Runs:         newMemoryRunStore(),
		Namespaces:   newMemoryNamespaceStore(),
		Schedules:    newMemoryScheduleStore(),
		Storage:      newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: lzStore,
	}
	return srv, lzStore
}

// --- List Zones ---

func TestListLandingZones_Empty_ReturnsEmptyList(t *testing.T) {
	srv, _ := newLandingTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListLandingZones_WithData_ReturnsAll(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads"}},
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "imports"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(2), body["total"])
}

func TestListLandingZones_FilterNamespace_ReturnsFiltered(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "analytics", Name: "raw"}},
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "marketing", Name: "csv-drops"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones?namespace=analytics", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(1), body["total"])
}

// --- Create Zone ---

func TestCreateLandingZone_Valid_Returns201(t *testing.T) {
	srv, _ := newLandingTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","name":"uploads","description":"Raw file drops"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "uploads", resp["name"])
	assert.Equal(t, "default", resp["namespace"])
	assert.Equal(t, "Raw file drops", resp["description"])
}

func TestCreateLandingZone_MissingName_Returns400(t *testing.T) {
	srv, _ := newLandingTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateLandingZone_UppercaseName_Returns400(t *testing.T) {
	srv, _ := newLandingTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","name":"MyZone"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestCreateLandingZone_Duplicate_Returns409(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	body := `{"namespace":"default","name":"uploads"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// --- Get Zone ---

func TestGetLandingZone_Exists_ReturnsZone(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads", Description: "test"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones/default/uploads", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "uploads", resp["name"])
}

func TestGetLandingZone_NotFound_Returns404(t *testing.T) {
	srv, _ := newLandingTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones/default/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Delete Zone ---

func TestDeleteLandingZone_Exists_Returns204(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/landing-zones/default/uploads", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- List Files ---

func TestListLandingFiles_Empty_ReturnsEmptyList(t *testing.T) {
	srv, store := newLandingTestServer()
	zoneID := uuid.New()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: zoneID, Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones/default/uploads/files", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

// --- Upload File ---

func TestUploadLandingFile_Valid_Returns201(t *testing.T) {
	srv, store := newLandingTestServer()
	zoneID := uuid.New()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: zoneID, Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "orders.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte("id,name\n1,Alice"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones/default/uploads/files", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	filename := resp["filename"].(string)
	assert.Contains(t, filename, "orders.csv")
	assert.Regexp(t, `^\d{8}_\d{6}_orders\.csv$`, filename)
	assert.Contains(t, resp["s3_path"].(string), "default/landing/uploads/")
	assert.Contains(t, resp["s3_path"].(string), "orders.csv")
}

func TestUploadLandingFile_MissingFile_Returns400(t *testing.T) {
	srv, store := newLandingTestServer()
	zoneID := uuid.New()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: zoneID, Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones/default/uploads/files", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- Get File ---

func TestGetLandingFile_Exists_ReturnsFile(t *testing.T) {
	srv, store := newLandingTestServer()
	zoneID := uuid.New()
	fileID := uuid.New()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: zoneID, Namespace: "default", Name: "uploads"}},
	}
	store.files = []domain.LandingFile{
		{ID: fileID, ZoneID: zoneID, Filename: "data.csv", S3Path: "default/landing/uploads/data.csv", SizeBytes: 100, ContentType: "text/csv"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones/default/uploads/files/"+fileID.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "data.csv", resp["filename"])
}

// --- Delete File ---

func TestDeleteLandingFile_Exists_Returns204(t *testing.T) {
	srv, store := newLandingTestServer()
	zoneID := uuid.New()
	fileID := uuid.New()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: zoneID, Namespace: "default", Name: "uploads"}},
	}
	store.files = []domain.LandingFile{
		{ID: fileID, ZoneID: zoneID, Filename: "data.csv", S3Path: "default/landing/uploads/data.csv"},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/landing-zones/default/uploads/files/"+fileID.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- List Samples ---

func TestListLandingSamples_Empty_ReturnsEmptyList(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones/default/uploads/samples", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["total"])
}

func TestListLandingSamples_ZoneNotFound_Returns404(t *testing.T) {
	srv, _ := newLandingTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones/default/nonexistent/samples", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Upload Sample ---

func TestUploadLandingSample_Valid_Returns201(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "sample.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte("id,name\n1,Alice"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones/default/uploads/samples", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "sample.csv", resp["filename"])
	assert.Equal(t, "default/landing/uploads/_samples/sample.csv", resp["path"])
	assert.Equal(t, "uploaded", resp["status"])
}

func TestUploadLandingSample_ZoneNotFound_Returns404(t *testing.T) {
	srv, _ := newLandingTestServer()
	router := api.NewRouter(srv)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "sample.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte("id,name\n1,Alice"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-zones/default/nonexistent/samples", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Delete Sample ---

func TestDeleteLandingSample_Valid_Returns204(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/landing-zones/default/uploads/samples/sample.csv", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteLandingSample_InvalidFilename_Returns400(t *testing.T) {
	srv, store := newLandingTestServer()
	store.zones = []api.LandingZoneListItem{
		{LandingZone: domain.LandingZone{ID: uuid.New(), Namespace: "default", Name: "uploads"}},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/landing-zones/default/uploads/samples/..%2F..%2Fevil.csv", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
