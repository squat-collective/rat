package api_test

import (
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

// memoryAuditStore is an in-memory audit store for testing.
type memoryAuditStore struct {
	mu      sync.Mutex
	entries []domain.AuditEntry
}

func (s *memoryAuditStore) Log(_ context.Context, userID, action, resource, detail, ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, domain.AuditEntry{
		UserID:   userID,
		Action:   action,
		Resource: resource,
		Detail:   detail,
		IP:       ip,
	})
	return nil
}

func (s *memoryAuditStore) DeleteOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func (s *memoryAuditStore) List(_ context.Context, limit, offset int) ([]domain.AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset >= len(s.entries) {
		return []domain.AuditEntry{}, nil
	}
	end := offset + limit
	if end > len(s.entries) {
		end = len(s.entries)
	}
	return s.entries[offset:end], nil
}

func TestAuditMiddleware_LogsMutatingRequests(t *testing.T) {
	store := &memoryAuditStore{}
	handler := api.AuditMiddleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST should be logged
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", http.NoBody)
	req.RemoteAddr = "1.2.3.4:1234"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// DELETE should be logged
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/test", http.NoBody)
	req.RemoteAddr = "1.2.3.4:1234"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Len(t, store.entries, 2)
	assert.Equal(t, "post", store.entries[0].Action)
	assert.Equal(t, "/api/v1/pipelines", store.entries[0].Resource)
	assert.Equal(t, "delete", store.entries[1].Action)
}

func TestAuditMiddleware_SkipsReadRequests(t *testing.T) {
	store := &memoryAuditStore{}
	handler := api.AuditMiddleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Empty(t, store.entries)
}

func TestHandleListAuditLog_ReturnsEntries(t *testing.T) {
	store := &memoryAuditStore{
		entries: []domain.AuditEntry{
			{ID: "1", UserID: "u-1", Action: "post", Resource: "/api/v1/pipelines"},
			{ID: "2", UserID: "u-1", Action: "delete", Resource: "/api/v1/pipelines/test"},
		},
	}

	srv := &api.Server{Audit: store}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", http.NoBody)
	rec := httptest.NewRecorder()

	srv.HandleListAuditLog(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var envelope struct {
		Entries []domain.AuditEntry `json:"entries"`
		Total   int                 `json:"total"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	require.NoError(t, err)
	assert.Len(t, envelope.Entries, 2)
	assert.Equal(t, 2, envelope.Total)
}

func TestHandleListAuditLog_NoStore_Returns404(t *testing.T) {
	srv := &api.Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", http.NoBody)
	rec := httptest.NewRecorder()

	srv.HandleListAuditLog(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
