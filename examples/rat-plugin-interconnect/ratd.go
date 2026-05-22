package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

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
}

func newRatdClient(baseURL string) *ratdClient {
	return &ratdClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// plugins returns every plugin ratd currently knows about.
func (c *ratdClient) plugins(ctx context.Context) ([]pluginInfo, error) {
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
