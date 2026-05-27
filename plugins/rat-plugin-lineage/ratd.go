package main

// ratdClient — HTTP wrapper around the ratd APIs this plugin needs:
// pipelines, runs, quality test counts, tables, landing zones, and
// pipeline source files. Same data the in-process ratd lineage handler
// reads via direct store calls; we just fetch it over HTTP.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ratdClient struct {
	baseURL string
	http    *http.Client
}

func newRatdClient(baseURL string) *ratdClient {
	return &ratdClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// pipeline is the slice of /api/v1/pipelines we need.
type pipeline struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	S3Path    string `json:"s3_path"`
}

type tableInfo struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Name      string `json:"name"`
	RowCount  int64  `json:"row_count"`
	SizeBytes int64  `json:"size_bytes"`
}

type landingZone struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	FileCount int    `json:"file_count"`
}

func (c *ratdClient) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ratd %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("ratd %s: not found", path)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ratd %s: HTTP %d", path, resp.StatusCode)
	}
	return raw, nil
}

func (c *ratdClient) listPipelines(ctx context.Context, ns string) ([]pipeline, error) {
	q := "/api/v1/pipelines"
	if ns != "" {
		q += "?namespace=" + ns
	}
	raw, err := c.get(ctx, q)
	if err != nil {
		return nil, err
	}
	var out struct {
		Pipelines []pipeline `json:"pipelines"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// Some installs return a bare array.
		var bare []pipeline
		if err2 := json.Unmarshal(raw, &bare); err2 == nil {
			return bare, nil
		}
		return nil, fmt.Errorf("decode pipelines: %w", err)
	}
	return out.Pipelines, nil
}

func (c *ratdClient) listTables(ctx context.Context, ns string) ([]tableInfo, error) {
	q := "/api/v1/tables"
	if ns != "" {
		q += "?namespace=" + ns
	}
	raw, err := c.get(ctx, q)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tables []tableInfo `json:"tables"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		var bare []tableInfo
		if err2 := json.Unmarshal(raw, &bare); err2 == nil {
			return bare, nil
		}
		return nil, fmt.Errorf("decode tables: %w", err)
	}
	return out.Tables, nil
}

func (c *ratdClient) listLandingZones(ctx context.Context, ns string) ([]landingZone, error) {
	q := "/api/v1/landing-zones"
	if ns != "" {
		q += "?namespace=" + ns
	}
	raw, err := c.get(ctx, q)
	if err != nil {
		// Landing zones may not be supported by ratd — soft-fail.
		return nil, nil
	}
	var out struct {
		Zones []landingZone `json:"zones"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		var bare []landingZone
		if err2 := json.Unmarshal(raw, &bare); err2 == nil {
			return bare, nil
		}
		return nil, nil
	}
	return out.Zones, nil
}

// listLatestRunPerPipeline returns the most recent run per pipeline,
// keyed by "ns.layer.name". ratd doesn't currently expose a batch
// "latest per pipeline" endpoint, so we fan out per-pipeline reads
// against /api/v1/runs?pipeline_id=...&limit=1.
func (c *ratdClient) listLatestRunPerPipeline(ctx context.Context, pipelines []pipeline) (map[string]*RunSummary, error) {
	out := make(map[string]*RunSummary, len(pipelines))
	sem := make(chan struct{}, maxConcurrentReads)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range pipelines {
		wg.Add(1)
		go func(p pipeline) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := c.get(ctx, fmt.Sprintf("/api/v1/runs?pipeline_id=%s&limit=1", p.ID))
			if err != nil {
				return
			}
			var resp struct {
				Runs []struct {
					ID         string  `json:"id"`
					Status     string  `json:"status"`
					StartedAt  *string `json:"started_at"`
					DurationMs *int64  `json:"duration_ms"`
				} `json:"runs"`
			}
			if json.Unmarshal(raw, &resp) != nil || len(resp.Runs) == 0 {
				return
			}
			r := resp.Runs[0]
			rs := &RunSummary{ID: r.ID, Status: r.Status}
			if r.StartedAt != nil {
				rs.StartedAt = *r.StartedAt
			}
			if r.DurationMs != nil {
				rs.DurationMs = *r.DurationMs
			}
			key := p.Namespace + "." + p.Layer + "." + p.Name
			mu.Lock()
			out[key] = rs
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return out, nil
}

// qualityTestCounts returns count of quality tests per pipeline,
// keyed by "ns.layer.name". Uses the per-pipeline endpoint.
func (c *ratdClient) qualityTestCounts(ctx context.Context, ns string) (map[string]int, error) {
	pipelines, err := c.listPipelines(ctx, ns)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(pipelines))
	sem := make(chan struct{}, maxConcurrentReads)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range pipelines {
		wg.Add(1)
		go func(p pipeline) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := c.get(ctx, fmt.Sprintf("/api/v1/pipelines/%s/%s/%s/tests", p.Namespace, p.Layer, p.Name))
			if err != nil {
				return
			}
			var resp struct {
				Tests []json.RawMessage `json:"tests"`
			}
			if json.Unmarshal(raw, &resp) != nil {
				return
			}
			if len(resp.Tests) == 0 {
				return
			}
			key := p.Namespace + "." + p.Layer + "." + p.Name
			mu.Lock()
			out[key] = len(resp.Tests)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return out, nil
}

// readFile fetches an S3 file via ratd's /api/v1/files/{path} endpoint.
func (c *ratdClient) readFile(ctx context.Context, path string) (string, error) {
	raw, err := c.get(ctx, "/api/v1/files/"+path)
	if err != nil {
		return "", err
	}
	var resp struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		// Some ratd versions return the file body directly.
		return string(raw), nil
	}
	return resp.Content, nil
}
