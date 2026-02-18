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

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryNamespaceStore is an in-memory NamespaceStore for tests.
type memoryNamespaceStore struct {
	mu         sync.Mutex
	namespaces []domain.Namespace
}

func newMemoryNamespaceStore() *memoryNamespaceStore {
	return &memoryNamespaceStore{
		namespaces: []domain.Namespace{
			{Name: "default", CreatedAt: time.Now()},
		},
	}
}

func (m *memoryNamespaceStore) ListNamespaces(_ context.Context) ([]domain.Namespace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]domain.Namespace, len(m.namespaces))
	copy(result, m.namespaces)
	return result, nil
}

func (m *memoryNamespaceStore) CreateNamespace(_ context.Context, name string, createdBy *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ns := range m.namespaces {
		if ns.Name == name {
			return fmt.Errorf("namespace %q already exists", name)
		}
	}
	m.namespaces = append(m.namespaces, domain.Namespace{Name: name, CreatedBy: createdBy, CreatedAt: time.Now()})
	return nil
}

func (m *memoryNamespaceStore) DeleteNamespace(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, ns := range m.namespaces {
		if ns.Name == name {
			m.namespaces = append(m.namespaces[:i], m.namespaces[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("namespace %q not found", name)
}

func (m *memoryNamespaceStore) UpdateNamespace(_ context.Context, name, description string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, ns := range m.namespaces {
		if ns.Name == name {
			m.namespaces[i].Description = description
			return nil
		}
	}
	return fmt.Errorf("namespace %q not found", name)
}

// newNsTestServer creates a Server with all stores for namespace tests.
func newNsTestServer() (*api.Server, *memoryNamespaceStore) {
	nsStore := newMemoryNamespaceStore()
	srv := &api.Server{
		Pipelines:  newMemoryPipelineStore(),
		Runs:       newMemoryRunStore(),
		Namespaces: nsStore,
		Schedules:  newMemoryScheduleStore(),
		Storage:    newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
	}
	return srv, nsStore
}

// --- List Namespaces ---

func TestListNamespaces_ReturnsDefault(t *testing.T) {
	srv, _ := newNsTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, float64(1), body["total"])

	namespaces := body["namespaces"].([]interface{})
	first := namespaces[0].(map[string]interface{})
	assert.Equal(t, "default", first["name"])
}

// --- Create Namespace ---

func TestCreateNamespace_ValidRequest_Returns201(t *testing.T) {
	srv, _ := newNsTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"analytics"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "analytics", resp["name"])
}

func TestCreateNamespace_MissingName_Returns400(t *testing.T) {
	srv, _ := newNsTestServer()
	router := api.NewRouter(srv)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateNamespace_UppercaseName_Returns400(t *testing.T) {
	srv, _ := newNsTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"Analytics"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestCreateNamespace_Duplicate_Returns409(t *testing.T) {
	srv, _ := newNsTestServer()
	router := api.NewRouter(srv)

	body := `{"name":"default"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// --- Delete Namespace ---

func TestDeleteNamespace_Exists_Returns204(t *testing.T) {
	srv, nsStore := newNsTestServer()
	nsStore.namespaces = append(nsStore.namespaces, domain.Namespace{Name: "analytics"})
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/namespaces/analytics", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteNamespace_DefaultProtected_Returns403(t *testing.T) {
	srv, _ := newNsTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/namespaces/default", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDeleteNamespace_NotFound_Returns500(t *testing.T) {
	srv, _ := newNsTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/namespaces/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
