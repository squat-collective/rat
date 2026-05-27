package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCloudProvider implements api.CloudProvider for handler tests.
// Routes the test's intent without touching the real plugin/Connect stack.
type mockCloudProvider struct {
	enabled bool
	creds   *domain.CloudCredentials
	err     error

	// lastCall records what the handler forwarded to the provider so tests
	// can assert that auth + query-param plumbing reached the right call.
	lastUserID    string
	lastNamespace string
}

func (m *mockCloudProvider) CloudEnabled() bool { return m.enabled }

func (m *mockCloudProvider) GetCredentials(_ context.Context, userID, namespace string) (*domain.CloudCredentials, error) {
	m.lastUserID = userID
	m.lastNamespace = namespace
	if m.err != nil {
		return nil, m.err
	}
	return m.creds, nil
}

// newCloudTestRouter builds a router with the given Cloud provider and an
// auth middleware that injects a fixed user identity. Pass nil for cloud to
// simulate "no provider plugin registered".
func newCloudTestRouter(cloud api.CloudProvider, authed bool) http.Handler {
	srv := &api.Server{
		Cloud: cloud,
		Auth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !authed {
					next.ServeHTTP(w, r)
					return
				}
				ctx := plugins.ContextWithUser(r.Context(), &domain.UserIdentity{
					UserID: "alice",
					Email:  "alice@rat.dev",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	}
	return api.NewRouter(srv)
}

func TestHandleGetCloudCredentials_NoProvider_Returns501(t *testing.T) {
	router := newCloudTestRouter(nil, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotImplemented, rec.Code)

	var body api.APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "NOT_IMPLEMENTED", body.Error.Code)
	assert.Contains(t, body.Error.Message, "no cloud provider plugin registered")
}

func TestHandleGetCloudCredentials_DisabledProvider_Returns501(t *testing.T) {
	// A provider that is wired but reports CloudEnabled() == false (e.g.,
	// catalog row exists but plugin is in "disabled" state).
	cp := &mockCloudProvider{enabled: false}
	router := newCloudTestRouter(cp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestHandleGetCloudCredentials_NoAuth_Returns401(t *testing.T) {
	cp := &mockCloudProvider{enabled: true}
	router := newCloudTestRouter(cp, false) // no user in context

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var body api.APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "UNAUTHENTICATED", body.Error.Code)
}

func TestHandleGetCloudCredentials_MissingNamespace_Returns400(t *testing.T) {
	cp := &mockCloudProvider{enabled: true}
	router := newCloudTestRouter(cp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body api.APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "INVALID_ARGUMENT", body.Error.Code)
	assert.Contains(t, body.Error.Message, "namespace")
}

func TestHandleGetCloudCredentials_InvalidNamespace_Returns400(t *testing.T) {
	cp := &mockCloudProvider{enabled: true}
	router := newCloudTestRouter(cp, true)

	// "Invalid Namespace!" has uppercase and a space — not a valid slug.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=Bad%20Name", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetCloudCredentials_HappyPath_ReturnsCredentials(t *testing.T) {
	// Use a future-relative expiry so the handler's "expired credentials"
	// guard (cloud.go) doesn't trip the wall-clock here.
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	cp := &mockCloudProvider{
		enabled: true,
		creds: &domain.CloudCredentials{
			AccessKey:    "AKIA-TEST",
			SecretKey:    "secret-shh",
			SessionToken: "session-xyz",
			Region:       "us-east-1",
			Expiry:       expiry,
		},
	}
	router := newCloudTestRouter(cp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.CloudCredentials
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "AKIA-TEST", got.AccessKey)
	assert.Equal(t, "secret-shh", got.SecretKey)
	assert.Equal(t, "session-xyz", got.SessionToken)
	assert.Equal(t, "us-east-1", got.Region)
	assert.True(t, got.Expiry.Equal(expiry), "expiry round-trip mismatch: %v vs %v", got.Expiry, expiry)

	// Confirm the handler forwarded the authenticated user and namespace.
	assert.Equal(t, "alice", cp.lastUserID)
	assert.Equal(t, "acme", cp.lastNamespace)
}

func TestHandleGetCloudCredentials_HappyPath_OmitsEmptySessionToken(t *testing.T) {
	cp := &mockCloudProvider{
		enabled: true,
		creds: &domain.CloudCredentials{
			AccessKey: "AKIA-LONGLIVED",
			SecretKey: "secret-shh",
			Region:    "us-east-1",
			// SessionToken intentionally empty — long-lived IAM user.
		},
	}
	router := newCloudTestRouter(cp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Use a raw map to verify the field is omitted from the wire JSON.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	_, hasSession := raw["session_token"]
	assert.False(t, hasSession, "session_token should be omitted when empty")
}

func TestHandleGetCloudCredentials_UpstreamError_Returns502(t *testing.T) {
	cp := &mockCloudProvider{
		enabled: true,
		err:     errors.New("STS AssumeRole denied"),
	}
	router := newCloudTestRouter(cp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleGetCloudCredentials_ExpiredCredentials_Returns502(t *testing.T) {
	// A buggy plugin returns credentials that are already expired. Without
	// validation the caller would only discover this on the next S3 call,
	// with a confusing AccessDenied / ExpiredToken error.
	cp := &mockCloudProvider{
		enabled: true,
		creds: &domain.CloudCredentials{
			AccessKey: "AKIA-STALE",
			SecretKey: "secret-shh",
			Region:    "us-east-1",
			Expiry:    time.Now().Add(-1 * time.Minute),
		},
	}
	router := newCloudTestRouter(cp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)

	var body api.APIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "UPSTREAM_ERROR", body.Error.Code)
	assert.Contains(t, body.Error.Message, "expired")
}

func TestHandleGetCloudCredentials_ZeroExpiry_PassesThrough(t *testing.T) {
	// Long-lived IAM users have no expiry (zero time.Time). The handler
	// must NOT treat that as "expired" — it means "no expiry".
	cp := &mockCloudProvider{
		enabled: true,
		creds: &domain.CloudCredentials{
			AccessKey: "AKIA-LONGLIVED",
			SecretKey: "secret-shh",
			Region:    "us-east-1",
			// Expiry intentionally zero — long-lived credential.
		},
	}
	router := newCloudTestRouter(cp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cloud/credentials?namespace=acme", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
