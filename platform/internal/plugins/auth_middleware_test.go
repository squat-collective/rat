package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	connect "connectrpc.com/connect"
	authv1 "github.com/rat-data/rat/platform/gen/auth/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handler that captures the user from context
func captureUserHandler(captured **authv1.UserIdentity) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		*captured = user
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddleware_NoPlugin_PassesThrough(t *testing.T) {
	reg := &Registry{edition: "community"}
	mw := reg.AuthMiddleware()

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called, "handler should be called without auth")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthMiddleware_ValidToken_SetsUserContext(t *testing.T) {
	mock := &mockAuthClient{
		authenticateFunc: func(_ context.Context, req *connect.Request[authv1.AuthenticateRequest]) (*connect.Response[authv1.AuthenticateResponse], error) {
			return connect.NewResponse(&authv1.AuthenticateResponse{
				Authenticated: true,
				User: &authv1.UserIdentity{
					UserId:      "u-789",
					Email:       "colette@rat.dev",
					DisplayName: "Colette",
					Roles:       []string{"editor"},
				},
			}), nil
		},
	}

	reg := &Registry{
		edition: "pro",
		auth:    &authPlugin{client: mock},
	}

	var captured *authv1.UserIdentity
	handler := reg.AuthMiddleware()(captureUserHandler(&captured))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, captured)
	assert.Equal(t, "u-789", captured.UserId)
	assert.Equal(t, "colette@rat.dev", captured.Email)
}

func TestAuthMiddleware_MissingToken_Returns401(t *testing.T) {
	reg := &Registry{
		edition: "pro",
		auth:    &authPlugin{client: &mockAuthClient{}},
	}

	handler := reg.AuthMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	assert.Contains(t, body["error"], "missing")
}

func TestAuthMiddleware_InvalidToken_Returns401(t *testing.T) {
	mock := &mockAuthClient{
		authenticateFunc: func(_ context.Context, req *connect.Request[authv1.AuthenticateRequest]) (*connect.Response[authv1.AuthenticateResponse], error) {
			return connect.NewResponse(&authv1.AuthenticateResponse{
				Authenticated: false,
				ErrorMessage:  "token expired",
			}), nil
		},
	}

	reg := &Registry{
		edition: "pro",
		auth:    &authPlugin{client: mock},
	}

	handler := reg.AuthMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer expired-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	assert.Equal(t, "token expired", body["error"])
}

func TestAuthMiddleware_PluginError_Returns401(t *testing.T) {
	mock := &mockAuthClient{
		authenticateFunc: func(_ context.Context, req *connect.Request[authv1.AuthenticateRequest]) (*connect.Response[authv1.AuthenticateResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("plugin down"))
		},
	}

	reg := &Registry{
		edition: "pro",
		auth:    &authPlugin{client: mock},
	}

	handler := reg.AuthMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestExtractBearerToken_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer my-token")
	assert.Equal(t, "my-token", extractBearerToken(req))
}

func TestExtractBearerToken_Missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	assert.Equal(t, "", extractBearerToken(req))
}

func TestExtractBearerToken_WrongScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	assert.Equal(t, "", extractBearerToken(req))
}
