package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	authv1 "github.com/rat-data/rat/platform/gen/auth/v1"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/stretchr/testify/assert"
)

// mockAuthorizer is a test Authorizer that returns a configurable result.
type mockAuthorizer struct {
	allowed bool
	err     error
}

func (m *mockAuthorizer) CanAccess(_ context.Context, _, _, _, _ string) (bool, error) {
	return m.allowed, m.err
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
	ctx := plugins.ContextWithUser(req.Context(), &authv1.UserIdentity{
		UserId: "bob",
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
	ctx := plugins.ContextWithUser(req.Context(), &authv1.UserIdentity{
		UserId: "alice",
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
	ctx := plugins.ContextWithUser(req.Context(), &authv1.UserIdentity{
		UserId: "alice",
		Email:  "alice@test.com",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.NotNil(t, store.pipelines[0].Owner)
	assert.Equal(t, "alice", *store.pipelines[0].Owner)
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
