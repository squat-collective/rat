package api_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginRegistryLive implements api.PluginRegistryLive for proxy tests.
type mockPluginRegistryLive struct {
	registry *plugins.Registry
}

func (m *mockPluginRegistryLive) Get(name string) *plugins.Plugin {
	return m.registry.Get(name)
}

func TestHandlePluginProxy_ForwardsToPlugin(t *testing.T) {
	// Start a mock upstream plugin server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","path":"` + r.URL.Path + `"}`))
	}))
	defer upstream.Close()

	// Register a plugin pointing to the upstream.
	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         upstream.URL,
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/test/path", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	body, _ := io.ReadAll(rec.Body)
	assert.Contains(t, string(body), `"/test/path"`)
}

func TestHandlePluginProxy_UnknownPlugin_Returns404(t *testing.T) {
	reg := plugins.NewRegistry("pro")
	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/nonexistent/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePluginProxy_DisabledPlugin_Returns503(t *testing.T) {
	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         "http://localhost:9999",
		Status:       domain.PluginStatusDisabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlePluginProxy_NoRegistry_Returns503(t *testing.T) {
	srv := &api.Server{} // No PluginRegistry.
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlePluginProxy_ForwardsQueryParams(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.RawQuery))
	}))
	defer upstream.Close()

	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         upstream.URL,
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/search?q=hello&limit=10", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body, _ := io.ReadAll(rec.Body)
	assert.Contains(t, string(body), "q=hello")
	assert.Contains(t, string(body), "limit=10")
}

func TestHandlePluginProxy_PluginRootPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`path:` + r.URL.Path))
	}))
	defer upstream.Close()

	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         upstream.URL,
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	// Request to plugin root (no trailing path).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body, _ := io.ReadAll(rec.Body)
	assert.Equal(t, "path:/", string(body))
}

// proxyWithUpstreamHeaders spins up an upstream that emits headers
// per upstreamHeaders, registers it as plugin "p", and returns the
// recorded response from a GET through the proxy.
func proxyWithUpstreamHeaders(t *testing.T, upstreamHeaders http.Header, status int) *httptest.ResponseRecorder {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, vs := range upstreamHeaders {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(status)
		// Do not write a body for redirect-style responses — the
		// 302-without-Location test case asserts on header behavior
		// only and a body is irrelevant.
	}))
	t.Cleanup(upstream.Close)

	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "p",
		Addr:         upstream.URL,
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
	}))
	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/p/anything", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// captureSlog swaps the default slog logger to a buffer-backed handler
// for the duration of the test and returns the buffer plus a restore
// func. Used to assert WARN logs for dropped headers.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf, func() { slog.SetDefault(prev) }
}

func TestPluginProxy_ResponseHeaderSanitization(t *testing.T) {
	type expect struct {
		present []string // header names that must be in the proxied response
		absent  []string // header names that must NOT be in the proxied response
		logHas  []string // substrings that must appear in slog output (WARN cases)
	}
	cases := []struct {
		name    string
		headers http.Header
		status  int
		expect  expect
	}{
		{
			name: "whitelist passes through",
			headers: http.Header{
				"Content-Type":        []string{"application/json"},
				"Cache-Control":       []string{"no-store"},
				"Etag":                []string{`"abc"`},
				"Last-Modified":       []string{"Wed, 21 Oct 2025 07:28:00 GMT"},
				"Vary":                []string{"Accept-Encoding"},
				"Content-Disposition": []string{"attachment; filename=x.csv"},
				"Accept-Ranges":       []string{"bytes"},
				"X-Request-ID":        []string{"req-123"},
			},
			status: http.StatusOK,
			expect: expect{
				present: []string{
					"Content-Type", "Cache-Control", "Etag", "Last-Modified",
					"Vary", "Content-Disposition", "Accept-Ranges", "X-Request-Id",
				},
			},
		},
		{
			name: "Set-Cookie is dropped and logged",
			headers: http.Header{
				"Content-Type": []string{"text/plain"},
				"Set-Cookie":   []string{"session=evil; Path=/; HttpOnly"},
			},
			status: http.StatusOK,
			expect: expect{
				present: []string{"Content-Type"},
				absent:  []string{"Set-Cookie"},
				logHas:  []string{"Set-Cookie", "plugin=p"},
			},
		},
		{
			name: "Location is dropped and logged on 302 (response goes through, Location is gone)",
			headers: http.Header{
				"Location": []string{"https://attacker.example.com/steal"},
			},
			// A 302 without Location is technically invalid HTTP, but
			// that's the plugin's problem — the portal must not honor
			// plugin-driven redirects. Documented in
			// sanitizeResponseHeaders.
			status: http.StatusFound,
			expect: expect{
				absent: []string{"Location"},
				logHas: []string{"Location", "plugin=p"},
			},
		},
		{
			name: "Content-Security-Policy is dropped",
			headers: http.Header{
				"Content-Type":            []string{"text/html"},
				"Content-Security-Policy": []string{"default-src 'none'"},
			},
			status: http.StatusOK,
			expect: expect{
				present: []string{"Content-Type"},
				absent:  []string{"Content-Security-Policy"},
			},
		},
		{
			name: "X-Custom-Foo (anything outside whitelist) is dropped",
			headers: http.Header{
				"Content-Type":              []string{"text/plain"},
				"X-Custom-Foo":              []string{"bar"},
				"Strict-Transport-Security": []string{"max-age=31536000"},
				"Access-Control-Allow-Origin": []string{"*"},
				"Authorization":             []string{"Bearer evil"},
				"Server":                    []string{"plugin/1.0"},
				"Via":                       []string{"1.1 plugin"},
				"WWW-Authenticate":          []string{"Basic"},
				"X-Forwarded-For":           []string{"1.2.3.4"},
			},
			status: http.StatusOK,
			expect: expect{
				present: []string{"Content-Type"},
				absent: []string{
					"X-Custom-Foo",
					"Strict-Transport-Security",
					"Access-Control-Allow-Origin",
					"Authorization",
					"Server",
					"Via",
					"Www-Authenticate", // canonical form
					"X-Forwarded-For",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf, restore := captureSlog(t)
			defer restore()

			rec := proxyWithUpstreamHeaders(t, tc.headers, tc.status)

			// Status code is forwarded unchanged — sanitization only
			// touches headers.
			assert.Equal(t, tc.status, rec.Code, "status should be forwarded unchanged")

			for _, h := range tc.expect.present {
				assert.NotEmpty(t, rec.Header().Get(h), "expected header %q to be present", h)
			}
			for _, h := range tc.expect.absent {
				assert.Empty(t, rec.Header().Get(h), "expected header %q to be stripped", h)
			}
			logs := buf.String()
			for _, frag := range tc.expect.logHas {
				assert.Contains(t, logs, frag, "expected log to contain %q", frag)
			}
		})
	}
}

func TestPluginProxy_ResponseHeaderSanitization_CaseInsensitive(t *testing.T) {
	// HTTP headers are case-insensitive on the wire; httptest's
	// ResponseRecorder uses http.Header which canonicalizes on Get,
	// but we still exercise the lowercase form here so the test
	// documents the contract.
	headers := http.Header{
		"set-cookie":  []string{"x=y"},
		"X-CUSTOM":    []string{"v"},
		"content-type": []string{"text/plain"},
	}
	_, restore := captureSlog(t)
	defer restore()

	rec := proxyWithUpstreamHeaders(t, headers, http.StatusOK)

	// Whitelisted (canonical: Content-Type) passes; the others are
	// stripped regardless of input casing.
	assert.NotEmpty(t, rec.Header().Get("Content-Type"))
	assert.Empty(t, rec.Header().Get("Set-Cookie"))
	assert.Empty(t, rec.Header().Get("X-Custom"))
	// And confirm via case-variant lookup the whitelist really matched.
	assert.True(t, strings.EqualFold(rec.Header().Get("content-type"), "text/plain"))
}
