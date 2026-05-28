package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginServiceClient implements pluginv1connect.PluginServiceClient
// for the new Registry-based tests. This mock covers the full interface
// that will be generated after `make proto` (HealthCheck, Describe,
// HandleEvent, Authenticate, Authorize).
type mockPluginServiceClient struct {
	healthCheckFunc  func(ctx context.Context, req *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error)
	describeFunc     func(ctx context.Context, req *connect.Request[pluginv1.DescribeRequest]) (*connect.Response[pluginv1.DescribeResponse], error)
	handleEventFunc  func(ctx context.Context, req *connect.Request[pluginv1.HandleEventRequest]) (*connect.Response[pluginv1.HandleEventResponse], error)
	authenticateFunc func(ctx context.Context, req *connect.Request[pluginv1.AuthenticateRequest]) (*connect.Response[pluginv1.AuthenticateResponse], error)
	authorizeFunc    func(ctx context.Context, req *connect.Request[pluginv1.AuthorizeRequest]) (*connect.Response[pluginv1.AuthorizeResponse], error)
}

func (m *mockPluginServiceClient) HealthCheck(ctx context.Context, req *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc(ctx, req)
	}
	return connect.NewResponse(&pluginv1.HealthCheckResponse{
		Status: pluginv1.Status_STATUS_SERVING,
	}), nil
}

func (m *mockPluginServiceClient) Describe(ctx context.Context, req *connect.Request[pluginv1.DescribeRequest]) (*connect.Response[pluginv1.DescribeResponse], error) {
	if m.describeFunc != nil {
		return m.describeFunc(ctx, req)
	}
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (m *mockPluginServiceClient) HandleEvent(ctx context.Context, req *connect.Request[pluginv1.HandleEventRequest]) (*connect.Response[pluginv1.HandleEventResponse], error) {
	if m.handleEventFunc != nil {
		return m.handleEventFunc(ctx, req)
	}
	return connect.NewResponse(&pluginv1.HandleEventResponse{}), nil
}

func (m *mockPluginServiceClient) Authenticate(ctx context.Context, req *connect.Request[pluginv1.AuthenticateRequest]) (*connect.Response[pluginv1.AuthenticateResponse], error) {
	if m.authenticateFunc != nil {
		return m.authenticateFunc(ctx, req)
	}
	return connect.NewResponse(&pluginv1.AuthenticateResponse{
		Authenticated: true,
		User: &pluginv1.UserIdentity{
			UserId:      "u-default",
			Email:       "default@rat.dev",
			DisplayName: "Default",
		},
	}), nil
}

func (m *mockPluginServiceClient) Authorize(ctx context.Context, req *connect.Request[pluginv1.AuthorizeRequest]) (*connect.Response[pluginv1.AuthorizeResponse], error) {
	if m.authorizeFunc != nil {
		return m.authorizeFunc(ctx, req)
	}
	return connect.NewResponse(&pluginv1.AuthorizeResponse{Allowed: true}), nil
}

// helper to register a mock auth plugin in a fresh registry.
func registryWithAuth(mock *mockPluginServiceClient) *Registry {
	reg := NewRegistry("pro")
	_ = reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	})
	return reg
}

// handler that captures the user from context
func captureUserHandler(captured **domain.UserIdentity) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		*captured = user
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddleware_NoPlugin_PassesThrough(t *testing.T) {
	reg := NewRegistry("community")
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
	mock := &mockPluginServiceClient{
		authenticateFunc: func(_ context.Context, req *connect.Request[pluginv1.AuthenticateRequest]) (*connect.Response[pluginv1.AuthenticateResponse], error) {
			return connect.NewResponse(&pluginv1.AuthenticateResponse{
				Authenticated: true,
				User: &pluginv1.UserIdentity{
					UserId:      "u-789",
					Email:       "colette@rat.dev",
					DisplayName: "Colette",
					Roles:       []string{"editor"},
				},
			}), nil
		},
	}

	reg := registryWithAuth(mock)

	var captured *domain.UserIdentity
	handler := reg.AuthMiddleware()(captureUserHandler(&captured))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, captured)
	assert.Equal(t, "u-789", captured.UserID)
	assert.Equal(t, "colette@rat.dev", captured.Email)
}

func TestAuthMiddleware_MissingToken_Returns401(t *testing.T) {
	mock := &mockPluginServiceClient{}
	reg := registryWithAuth(mock)

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
	mock := &mockPluginServiceClient{
		authenticateFunc: func(_ context.Context, req *connect.Request[pluginv1.AuthenticateRequest]) (*connect.Response[pluginv1.AuthenticateResponse], error) {
			return connect.NewResponse(&pluginv1.AuthenticateResponse{
				Authenticated: false,
				ErrorMessage:  "token expired",
			}), nil
		},
	}

	reg := registryWithAuth(mock)

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
	mock := &mockPluginServiceClient{
		authenticateFunc: func(_ context.Context, req *connect.Request[pluginv1.AuthenticateRequest]) (*connect.Response[pluginv1.AuthenticateResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("plugin down"))
		},
	}

	reg := registryWithAuth(mock)

	handler := reg.AuthMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_AuthenticateTimeout_Returns401AndLogsWarn(t *testing.T) {
	// Redirect slog to a buffer so we can assert the WARN log content.
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	// Mock Authenticate that blocks until the caller's context is cancelled.
	// In production the auth plugin would simply be slow/hung; this mock
	// captures that by honouring the deadline injected by the middleware.
	// If the middleware did NOT inject a deadline, this would block for 10s
	// and the test would fail on the elapsed-time assertion.
	mock := &mockPluginServiceClient{
		authenticateFunc: func(ctx context.Context, _ *connect.Request[pluginv1.AuthenticateRequest]) (*connect.Response[pluginv1.AuthenticateResponse], error) {
			select {
			case <-ctx.Done():
				return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
			case <-time.After(10 * time.Second):
				t.Fatal("mock Authenticate was not cancelled by the middleware deadline")
				return nil, nil
			}
		},
	}

	reg := NewRegistry("pro")
	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth-keycloak",
		Addr:         "http://auth-keycloak:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	}))

	handler := reg.AuthMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream handler must not be called on auth timeout")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Returns 401 promptly — within the timeout window plus a small slack
	// for goroutine scheduling. If the deadline weren't honoured this would
	// be ~10s.
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Less(t, elapsed, authPluginRPCTimeout+2*time.Second,
		"middleware must short-circuit on auth plugin timeout (took %s)", elapsed)

	// Body must not leak the plugin name.
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "authentication timed out", body["error"])
	assert.NotContains(t, body["error"], "auth-keycloak")

	// WARN log must name the plugin so operators can pinpoint the culprit.
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"level":"WARN"`)
	assert.Contains(t, logOutput, "auth plugin Authenticate timed out")
	assert.Contains(t, logOutput, `"plugin":"auth-keycloak"`)
}

func TestIsDeadlineExceeded(t *testing.T) {
	assert.False(t, isDeadlineExceeded(nil))
	assert.False(t, isDeadlineExceeded(errors.New("some other error")))
	assert.True(t, isDeadlineExceeded(context.DeadlineExceeded))
	// Wrapped context.DeadlineExceeded (e.g. via fmt.Errorf("%w", ...)).
	assert.True(t, isDeadlineExceeded(errors.Join(context.DeadlineExceeded, errors.New("wrapped"))))
	// ConnectRPC deadline-exceeded surfaced as a connect.Error.
	connectErr := connect.NewError(connect.CodeDeadlineExceeded, errors.New("timeout"))
	assert.True(t, isDeadlineExceeded(connectErr))
	// Other connect codes don't count.
	otherConnect := connect.NewError(connect.CodeUnavailable, errors.New("down"))
	assert.False(t, isDeadlineExceeded(otherConnect))
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
