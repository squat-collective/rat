package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// pluginCacheTTL is how long the broker keeps a snapshot of the plugin
// list before hitting ratd again. Every invoke calls plugins() to verify
// the provider is healthy, so without caching this hammers ratd at the
// broker's own request rate (every MCP poll = +1 hit). 5 seconds is short
// enough that a plugin going unhealthy is reflected almost immediately,
// long enough to coalesce burst traffic into a single upstream call.
const pluginCacheTTL = 5 * time.Second

// pluginInfo is one plugin as ratd reports it on GET /api/v1/plugins — the
// node list for the mesh.
type pluginInfo struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Version      string   `json:"version"`
	Healthy      bool     `json:"healthy"`
	Capabilities []string `json:"capabilities"`
}

// ratdClient reads ratd's plugin registry and proxies brokered calls to
// provider plugins through ratd's plugin route proxy.
type ratdClient struct {
	baseURL string
	http    *http.Client

	// Plugin-list cache. plugins() is called on EVERY invoke for the
	// health-check, so a steady stream of broker traffic would otherwise
	// hammer ratd's /api/v1/plugins endpoint.
	cacheMu     sync.Mutex
	cachedList  []pluginInfo
	cachedAt    time.Time
}

func newRatdClient(baseURL string) *ratdClient {
	return &ratdClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// plugins returns every plugin ratd currently knows about, served from a
// short TTL cache. See pluginCacheTTL.
func (c *ratdClient) plugins(ctx context.Context) ([]pluginInfo, error) {
	c.cacheMu.Lock()
	if c.cachedList != nil && time.Since(c.cachedAt) < pluginCacheTTL {
		out := c.cachedList
		c.cacheMu.Unlock()
		return out, nil
	}
	c.cacheMu.Unlock()

	list, err := c.fetchPlugins(ctx)
	if err != nil {
		// On error, surface the most recent cache rather than failing the
		// caller — better to route to a possibly-stale provider than to
		// reject the broker call entirely.
		c.cacheMu.Lock()
		stale := c.cachedList
		c.cacheMu.Unlock()
		if stale != nil {
			return stale, nil
		}
		return nil, err
	}

	c.cacheMu.Lock()
	c.cachedList = list
	c.cachedAt = time.Now()
	c.cacheMu.Unlock()
	return list, nil
}

// fetchPlugins is the raw GET — kept separate so plugins() can wrap it
// with caching cleanly.
func (c *ratdClient) fetchPlugins(ctx context.Context) ([]pluginInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/plugins", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))

	// ratd may return a bare array or an object with a "plugins" field —
	// accept either.
	var list []pluginInfo
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		Plugins []pluginInfo `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Plugins, nil
}

// invoke forwards a brokered call to a provider plugin through ratd's route
// proxy (/api/v1/x/{provider}/...).
func (c *ratdClient) invoke(
	ctx context.Context, provider, method, path string, body []byte,
) (int, []byte, error) {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(
		ctx, method, c.baseURL+"/api/v1/x/"+provider+path, rdr,
	)
	if err != nil {
		return 0, nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	return resp.StatusCode, out, nil
}
