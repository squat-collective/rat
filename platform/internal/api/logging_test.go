package api_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLogs installs a JSON slog handler that writes to a buffer,
// runs fn, then restores the previous default logger. Returns the
// captured log output as a string.
func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	fn()

	return buf.String()
}

func TestRequestLogger_200_LogsInfoLevel(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, output, `"level":"INFO"`)
	assert.Contains(t, output, `"msg":"request completed"`)
	assert.Contains(t, output, `"method":"GET"`)
	assert.Contains(t, output, `"path":"/api/v1/pipelines"`)
	assert.Contains(t, output, `"status":200`)
}

func TestRequestLogger_400_LogsWarnLevel(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, output, `"level":"WARN"`)
	assert.Contains(t, output, `"status":400`)
}

func TestRequestLogger_404_LogsWarnLevel(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Contains(t, output, `"level":"WARN"`)
	assert.Contains(t, output, `"status":404`)
}

func TestRequestLogger_500_LogsErrorLevel(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, output, `"level":"ERROR"`)
	assert.Contains(t, output, `"status":500`)
}

func TestRequestLogger_HealthEndpoint_SkipsLogging(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/health", "/health/live"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
			rec := httptest.NewRecorder()

			output := captureLogs(t, func() {
				handler.ServeHTTP(rec, req)
			})

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Empty(t, output, "health endpoint should not produce log output")
		})
	}
}

func TestRequestLogger_HealthReady_NotSkipped(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, output, `"msg":"request completed"`, "/health/ready should still be logged")
}

func TestRequestLogger_IncludesRequestID(t *testing.T) {
	// Wrap with RequestID middleware first, then RequestLogger.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := api.RequestID(api.RequestLogger(inner))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/features", http.NoBody)
	req.Header.Set("X-Request-ID", "test-req-123")
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Contains(t, output, `"request_id":"test-req-123"`)
}

func TestRequestLogger_IncludesResponseSize(t *testing.T) {
	body := `{"data":"hello world"}`
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Contains(t, output, `"response_size":22`)
}

func TestRequestLogger_IncludesDuration(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Contains(t, output, `"duration":`)
}

func TestRequestLogger_DefaultStatus200_WhenHandlerDoesNotCallWriteHeader(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handler writes body without explicit WriteHeader — Go defaults to 200.
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Contains(t, output, `"status":200`)
	assert.Contains(t, output, `"level":"INFO"`)
}

func TestRequestLogger_LogsMethod(t *testing.T) {
	handler := api.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Contains(t, output, `"method":"POST"`)
	assert.Contains(t, output, `"status":201`)
}

func TestRequestLogger_IntegrationWithRouter_200(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		router.ServeHTTP(rec, req)
	})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, output, `"level":"INFO"`)
	assert.Contains(t, output, `"path":"/api/v1/namespaces"`)
	assert.Contains(t, output, `"status":200`)
	assert.Contains(t, output, `"request_id"`)
}

func TestRequestLogger_IntegrationWithRouter_HealthSkipped(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	output := captureLogs(t, func() {
		router.ServeHTTP(rec, req)
	})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, output, "health endpoint should not produce log output through the router")
}
