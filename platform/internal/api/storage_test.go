package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryStorageStore is an in-memory StorageStore for tests.
type memoryStorageStore struct {
	mu    sync.Mutex
	files map[string][]byte // path → content
}

func newMemoryStorageStore() *memoryStorageStore {
	return &memoryStorageStore{files: make(map[string][]byte)}
}

func (m *memoryStorageStore) ListFiles(_ context.Context, prefix string) ([]api.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []api.FileInfo
	for path, content := range m.files {
		if prefix == "" || strings.HasPrefix(path, prefix) {
			result = append(result, api.FileInfo{
				Path:     path,
				Size:     int64(len(content)),
				Modified: time.Now(),
			})
		}
	}
	return result, nil
}

func (m *memoryStorageStore) ReadFile(_ context.Context, path string) (*api.FileContent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, ok := m.files[path]
	if !ok {
		return nil, nil
	}
	return &api.FileContent{
		Path:     path,
		Content:  string(content),
		Size:     int64(len(content)),
		Modified: time.Now(),
	}, nil
}

func (m *memoryStorageStore) WriteFile(_ context.Context, path string, content []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.files[path] = content
	return "mock-version-id", nil
}

func (m *memoryStorageStore) StatFile(_ context.Context, path string) (*api.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, ok := m.files[path]
	if !ok {
		return nil, nil
	}
	return &api.FileInfo{
		Path:      path,
		Size:      int64(len(content)),
		Modified:  time.Now(),
		VersionID: "mock-version-id",
	}, nil
}

func (m *memoryStorageStore) ReadFileVersion(_ context.Context, path, versionID string) (*api.FileContent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, ok := m.files[path]
	if !ok {
		return nil, nil
	}
	return &api.FileContent{
		Path:      path,
		Content:   string(content),
		Size:      int64(len(content)),
		Modified:  time.Now(),
		VersionID: versionID,
	}, nil
}

func (m *memoryStorageStore) DeleteFile(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.files[path]; !ok {
		return fmt.Errorf("file not found: %s", path)
	}
	delete(m.files, path)
	return nil
}

// newStorageTestServer creates a Server with all stores.
func newStorageTestServer() (*api.Server, *memoryStorageStore) {
	storageStore := newMemoryStorageStore()
	srv := &api.Server{
		Pipelines:    newMemoryPipelineStore(),
		Runs:         newMemoryRunStore(),
		Namespaces:   newMemoryNamespaceStore(),
		Schedules:    newMemoryScheduleStore(),
		Storage:      storageStore,
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, storageStore
}

// --- List Files ---

func TestListFiles_EmptyStore_ReturnsEmptyList(t *testing.T) {
	srv, _ := newStorageTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files?prefix=default/", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	files := body["files"].([]interface{})
	assert.Len(t, files, 0)
}

func TestListFiles_NoPrefix_Returns400(t *testing.T) {
	srv, _ := newStorageTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListFiles_WithPrefix_ReturnsFiltered(t *testing.T) {
	srv, store := newStorageTestServer()
	store.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("SELECT 1")
	store.files["default/pipelines/silver/orders/config.yaml"] = []byte("key: val")
	store.files["default/pipelines/bronze/events/pipeline.sql"] = []byte("SELECT 2")
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files?prefix=default/pipelines/silver/", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	files := body["files"].([]interface{})
	assert.Len(t, files, 2)
}

func TestListFiles_WithExclude_FiltersOutMatchingSegments(t *testing.T) {
	srv, store := newStorageTestServer()
	store.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("SELECT 1")
	store.files["default/landing/uploads/data.csv"] = []byte("a,b,c")
	store.files["default/data/iceberg/orders/v1.parquet"] = []byte("parquet")
	store.files["default/docs/readme.md"] = []byte("# Readme")
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files?prefix=default/&exclude=landing,data", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	files := body["files"].([]interface{})
	assert.Len(t, files, 2)

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.(map[string]interface{})["path"].(string)
	}
	assert.ElementsMatch(t, []string{
		"default/pipelines/silver/orders/pipeline.sql",
		"default/docs/readme.md",
	}, paths)
}

// --- Read File ---

func TestReadFile_Exists_ReturnsContent(t *testing.T) {
	srv, store := newStorageTestServer()
	store.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("SELECT * FROM raw_orders")
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/default/pipelines/silver/orders/pipeline.sql", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM raw_orders", body["content"])
}

func TestReadFile_NotFound_Returns404(t *testing.T) {
	srv, _ := newStorageTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/nonexistent.sql", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Write File ---

func TestWriteFile_NewFile_ReturnsWritten(t *testing.T) {
	srv, store := newStorageTestServer()
	router := api.NewRouter(srv)

	body := `{"content":"SELECT * FROM orders WHERE id > 0"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/files/default/pipelines/gold/revenue/pipeline.sql", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "written", resp["status"])

	// Verify file was stored
	assert.Equal(t, []byte("SELECT * FROM orders WHERE id > 0"), store.files["default/pipelines/gold/revenue/pipeline.sql"])
}

func TestWriteFile_OverwriteExisting_ReturnsWritten(t *testing.T) {
	srv, store := newStorageTestServer()
	store.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("old content")
	router := api.NewRouter(srv)

	body := `{"content":"new content"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/files/default/pipelines/silver/orders/pipeline.sql", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []byte("new content"), store.files["default/pipelines/silver/orders/pipeline.sql"])
}

// --- Delete File ---

func TestDeleteFile_Exists_Returns204(t *testing.T) {
	srv, store := newStorageTestServer()
	store.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("content")
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files/default/pipelines/silver/orders/pipeline.sql", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	_, exists := store.files["default/pipelines/silver/orders/pipeline.sql"]
	assert.False(t, exists)
}

func TestDeleteFile_NotFound_Returns500(t *testing.T) {
	srv, _ := newStorageTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files/nonexistent.sql", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// --- Upload File ---

// createMultipartRequest builds a multipart/form-data request for upload tests.
func createMultipartRequest(t *testing.T, path, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if path != "" {
		require.NoError(t, writer.WriteField("path", path))
	}

	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = part.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestUploadFile_Valid_Returns201(t *testing.T) {
	srv, store := newStorageTestServer()
	router := api.NewRouter(srv)

	req := createMultipartRequest(t, "default/pipelines/silver/orders/pipeline.sql", "pipeline.sql", "SELECT * FROM raw_orders")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "default/pipelines/silver/orders/pipeline.sql", resp["path"])
	assert.Equal(t, "pipeline.sql", resp["filename"])
	assert.Equal(t, "uploaded", resp["status"])

	// Verify file was stored
	assert.Equal(t, []byte("SELECT * FROM raw_orders"), store.files["default/pipelines/silver/orders/pipeline.sql"])
}

func TestUploadFile_MissingPath_Returns400(t *testing.T) {
	srv, _ := newStorageTestServer()
	router := api.NewRouter(srv)

	req := createMultipartRequest(t, "", "pipeline.sql", "SELECT 1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadFile_MissingFile_Returns400(t *testing.T) {
	srv, _ := newStorageTestServer()
	router := api.NewRouter(srv)

	// Build a multipart request with path but no file field
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	require.NoError(t, writer.WriteField("path", "default/test.sql"))
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadFile_OverwriteExisting_Returns201(t *testing.T) {
	srv, store := newStorageTestServer()
	store.files["default/pipelines/silver/orders/pipeline.sql"] = []byte("old content")
	router := api.NewRouter(srv)

	req := createMultipartRequest(t, "default/pipelines/silver/orders/pipeline.sql", "pipeline.sql", "new content via upload")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, []byte("new content via upload"), store.files["default/pipelines/silver/orders/pipeline.sql"])
}

// --- Write File + Draft Dirty ---

func TestWriteFile_SetsDraftDirty(t *testing.T) {
	srv, _ := newStorageTestServer()
	// Seed a pipeline in the pipeline store
	pipelineStore := srv.Pipelines.(*memoryPipelineStore)
	pipelineStore.pipelines = append(pipelineStore.pipelines, domain.Pipeline{
		Namespace: "default", Layer: domain.LayerSilver, Name: "orders", Type: "sql",
	})
	router := api.NewRouter(srv)

	body := `{"content":"SELECT * FROM updated_orders"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/files/default/pipelines/silver/orders/pipeline.sql", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify draft_dirty was set
	p, _ := pipelineStore.GetPipeline(context.Background(), "default", "silver", "orders")
	require.NotNil(t, p)
	assert.True(t, p.DraftDirty)

	// Verify version_id is returned
	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["version_id"])
}

func TestWriteFile_NonPipelinePath_NoDraftDirty(t *testing.T) {
	srv, _ := newStorageTestServer()
	router := api.NewRouter(srv)

	body := `{"content":"some data"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/files/default/docs/readme.md", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should succeed without error (no pipeline to mark dirty)
	assert.Equal(t, http.StatusOK, rec.Code)
}
