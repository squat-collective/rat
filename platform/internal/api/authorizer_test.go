package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAuthorizer is a test Authorizer that returns a configurable result.
// If allowedIDs is non-nil, Filter restricts to that set; otherwise Filter
// passes through (allowed=true) or returns empty (allowed=false).
type mockAuthorizer struct {
	allowed    bool
	err        error
	allowedIDs map[string]bool
}

func (m *mockAuthorizer) CanAccess(_ context.Context, _, _, resourceID, _ string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	if m.allowedIDs != nil {
		return m.allowedIDs[resourceID], nil
	}
	return m.allowed, nil
}

func (m *mockAuthorizer) Filter(_ context.Context, _, _, _ string, ids []string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.allowedIDs != nil {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if m.allowedIDs[id] {
				out = append(out, id)
			}
		}
		return out, nil
	}
	if m.allowed {
		return ids, nil
	}
	return nil, nil
}

func TestNoopAuthorizer_AlwaysAllows(t *testing.T) {
	a := api.NoopAuthorizer{}
	allowed, err := a.CanAccess(context.Background(), "user1", "pipeline", "p1", "write")
	assert.NoError(t, err)
	assert.True(t, allowed)
}

func TestRequireAccess_NoUser_Allows(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "orders", Type: "sql"},
	}
	srv.Authorizer = &mockAuthorizer{allowed: false} // even when authorizer denies
	router := api.NewRouter(srv)

	// No auth context → should allow (community mode)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/bronze/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestRequireAccess_AuthorizerDenies_Returns403(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "orders", Type: "sql"},
	}
	srv.Authorizer = &mockAuthorizer{allowed: false}
	router := api.NewRouter(srv)

	// With user context → authorizer denies → 403
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/bronze/orders", http.NoBody)
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{
		UserID: "bob",
		Email:  "bob@test.com",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireAccess_AuthorizerAllows_Proceeds(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{Namespace: "default", Layer: domain.LayerBronze, Name: "orders", Type: "sql", Description: "old"},
	}
	srv.Authorizer = &mockAuthorizer{allowed: true}
	router := api.NewRouter(srv)

	body := `{"description":"new desc"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipelines/default/bronze/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{
		UserID: "alice",
		Email:  "alice@test.com",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCreatePipeline_SetsOwnerFromContext(t *testing.T) {
	srv, store := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze","name":"orders","type":"sql"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{
		UserID: "alice",
		Email:  "alice@test.com",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.NotNil(t, store.pipelines[0].Owner)
	assert.Equal(t, "alice", *store.pipelines[0].Owner)
}

// TestListPipelines_FilterByAccess is the regression for the Pro
// enforcement bypass in the read path. Without filterAccess, ListPipelines
// returned every pipeline regardless of the user's grants.
func TestListPipelines_FilterByAccess(t *testing.T) {
	srv, store := newTestServer()
	visible := uuid.New()
	hidden := uuid.New()
	store.pipelines = []domain.Pipeline{
		{ID: visible, Namespace: "default", Layer: domain.LayerBronze, Name: "visible", Type: "sql"},
		{ID: hidden, Namespace: "default", Layer: domain.LayerBronze, Name: "hidden", Type: "sql"},
	}
	srv.Authorizer = &mockAuthorizer{allowedIDs: map[string]bool{visible.String(): true}}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{UserID: "alice"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	pipelines := body["pipelines"].([]interface{})
	require.Len(t, pipelines, 1, "hidden pipeline should be filtered out")
	assert.Equal(t, "visible", pipelines[0].(map[string]interface{})["name"])
	// Total reflects the visible count, not the raw SQL count.
	assert.Equal(t, float64(1), body["total"])
}

func TestListPipelines_NoUser_ReturnsAll(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "a", Type: "sql"},
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "b", Type: "sql"},
	}
	srv.Authorizer = &mockAuthorizer{allowed: false} // would deny if it ran
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Len(t, body["pipelines"], 2)
}

func TestGetPipeline_AuthorizerDenies_Returns403(t *testing.T) {
	srv, store := newTestServer()
	store.pipelines = []domain.Pipeline{
		{ID: uuid.New(), Namespace: "default", Layer: domain.LayerBronze, Name: "orders", Type: "sql"},
	}
	srv.Authorizer = &mockAuthorizer{allowed: false}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/orders", http.NoBody)
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{UserID: "bob"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestListRuns_FiltersByPipelineAccess(t *testing.T) {
	srv, pipelineStore := newTestServer()
	visiblePipelineID := uuid.New()
	hiddenPipelineID := uuid.New()
	pipelineStore.pipelines = []domain.Pipeline{
		{ID: visiblePipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "visible", Type: "sql"},
		{ID: hiddenPipelineID, Namespace: "default", Layer: domain.LayerBronze, Name: "hidden", Type: "sql"},
	}
	runStore := srv.Runs.(*memoryRunStore)
	runStore.runs = []domain.Run{
		{ID: uuid.New(), PipelineID: visiblePipelineID, Status: domain.RunStatusSuccess},
		{ID: uuid.New(), PipelineID: hiddenPipelineID, Status: domain.RunStatusSuccess},
		{ID: uuid.New(), PipelineID: visiblePipelineID, Status: domain.RunStatusFailed},
	}
	srv.Authorizer = &mockAuthorizer{allowedIDs: map[string]bool{visiblePipelineID.String(): true}}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", http.NoBody)
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{UserID: "alice"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	runs := body["runs"].([]interface{})
	assert.Len(t, runs, 2, "only runs from the visible pipeline should remain")
}

func TestCreatePipeline_NoUser_NilOwner(t *testing.T) {
	srv, store := newTestServer()
	router := api.NewRouter(srv)

	body := `{"namespace":"default","layer":"bronze","name":"orders","type":"sql"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Nil(t, store.pipelines[0].Owner)
}
