package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// AuditStore provides audit logging and retrieval.
type AuditStore interface {
	Log(ctx context.Context, userID, action, resource, detail, ip string) error
	List(ctx context.Context, limit, offset int) ([]domain.AuditEntry, error)
	DeleteOlderThan(ctx context.Context, olderThan time.Time) (int, error)
}

// AuditMiddleware logs mutating API requests (POST, PUT, DELETE) to the audit store.
// Audit entries are captured before calling the next handler so that logging
// does not race with the response being sent. The request context is still
// valid at this point; after the handler returns, the context may be cancelled.
func AuditMiddleware(store AuditStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit mutating requests
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
				userID := "anonymous"
				if user := plugins.UserFromContext(r.Context()); user != nil {
					userID = user.UserId
				}

				action := strings.ToLower(r.Method)
				resource := r.URL.Path
				ip := r.Header.Get("X-Real-Ip")
				if ip == "" {
					ip = r.RemoteAddr
				}

				if err := store.Log(r.Context(), userID, action, resource, "", ip); err != nil {
					slog.Warn("audit log failed", "error", err)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// MountAuditRoutes registers audit log API endpoints.
func MountAuditRoutes(r interface{ Get(string, http.HandlerFunc) }, srv *Server) {
	r.Get("/audit", srv.HandleListAuditLog)
}

// HandleListAuditLog returns recent audit log entries.
func (s *Server) HandleListAuditLog(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		errorJSON(w, "audit logging not enabled", "NOT_FOUND", http.StatusNotFound)
		return
	}

	limit, offset := parsePagination(r)
	entries, err := s.Audit.List(r.Context(), limit, offset)
	if err != nil {
		internalError(w, "failed to list audit log", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"total":   len(entries),
	})
}
