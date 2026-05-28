package api_test

import (
	"bytes"
	"context"
	"encoding/json"
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

// mockFailedMergesStore captures Create() calls so tests can assert on
// what the handler persisted. It also implements RecentBranchNames so the
// same mock works for the reaper tests if reused.
type mockFailedMergesStore struct {
	mu        sync.Mutex
	created   []domain.FailedMerge
	createErr error
}

func (m *mockFailedMergesStore) Create(_ context.Context, fm domain.FailedMerge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	m.created = append(m.created, fm)
	return nil
}

func (m *mockFailedMergesStore) RecentBranchNames(_ context.Context, _ time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.created))
	for _, fm := range m.created {
		out = append(out, fm.BranchName)
	}
	return out, nil
}

func TestFailedMergesEndpoint_ValidPayload_Persists(t *testing.T) {
	store := &mockFailedMergesStore{}
	srv := &api.Server{FailedMerges: store}
	router := api.NewInternalRouter(srv)

	body := domain.FailedMerge{
		RunID:        "00000000-0000-0000-0000-000000000123",
		BranchName:   "run-00000000-0000-0000-0000-000000000123",
		SourceHash:   "deadbeef",
		TargetHash:   "cafebabe",
		ErrorKind:    "conflict_exhausted",
		ErrorMessage: "HTTP 409: Conflict",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/failed-merges", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, store.created, 1)
	assert.Equal(t, body.BranchName, store.created[0].BranchName)
	assert.Equal(t, "conflict_exhausted", store.created[0].ErrorKind)
}

func TestFailedMergesEndpoint_MissingRequiredFields_Returns400(t *testing.T) {
	store := &mockFailedMergesStore{}
	srv := &api.Server{FailedMerges: store}
	router := api.NewInternalRouter(srv)

	body := domain.FailedMerge{
		RunID:      "00000000-0000-0000-0000-000000000123",
		BranchName: "", // missing — should be rejected
		ErrorKind:  "permanent_4xx",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/failed-merges", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, store.created)
}

func TestFailedMergesEndpoint_NoStoreConfigured_Returns200(t *testing.T) {
	// Dev mode without Postgres — handler should accept gracefully
	// so the runner doesn't loop on what looks like a real network error.
	srv := &api.Server{FailedMerges: nil}
	router := api.NewInternalRouter(srv)

	body := domain.FailedMerge{
		RunID:        "00000000-0000-0000-0000-000000000123",
		BranchName:   "run-abc",
		ErrorKind:    "transient_exhausted",
		ErrorMessage: "URLError: Connection refused",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/failed-merges", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestFailedMergesEndpoint_PublicRouterDoesNotExpose(t *testing.T) {
	// Trust-boundary check: this is an internal-only route.
	srv := &api.Server{FailedMerges: &mockFailedMergesStore{}}
	router := api.NewRouter(srv)

	body := domain.FailedMerge{
		RunID:        "00000000-0000-0000-0000-000000000123",
		BranchName:   "run-abc",
		ErrorKind:    "permanent_4xx",
		ErrorMessage: "HTTP 400",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/failed-merges", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"public router must NOT expose the internal failed-merges callback")
}
