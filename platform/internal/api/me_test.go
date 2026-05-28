package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMe_Authenticated_ReturnsUser(t *testing.T) {
	srv := &api.Server{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", http.NoBody)
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{
		UserID:      "usr_123",
		Email:       "tom@example.com",
		DisplayName: "Tom",
		Roles:       []string{"admin", "editor"},
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.HandleMe(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body api.MeResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "usr_123", body.UserID)
	assert.Equal(t, "tom@example.com", body.Email)
	assert.Equal(t, "Tom", body.DisplayName)
	assert.Equal(t, []string{"admin", "editor"}, body.Roles)
}

func TestHandleMe_Unauthenticated_Returns401(t *testing.T) {
	srv := &api.Server{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", http.NoBody)
	rec := httptest.NewRecorder()
	srv.HandleMe(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var body api.APIError
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "UNAUTHENTICATED", body.Error.Code)
	assert.Equal(t, "AUTHENTICATION", body.Error.Type)
}

func TestHandleMe_EmptyRoles_ReturnsEmptyArray(t *testing.T) {
	srv := &api.Server{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", http.NoBody)
	ctx := plugins.ContextWithUser(req.Context(), &domain.UserIdentity{
		UserID:      "usr_456",
		Email:       "alice@example.com",
		DisplayName: "Alice",
		// Roles is nil (no roles assigned)
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.HandleMe(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body api.MeResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "usr_456", body.UserID)
	assert.Equal(t, []string{}, body.Roles)
}
