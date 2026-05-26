package main

// ratdClient is the thin wrapper around ratd's REST API that we use to
// stamp pipelines/files/schedules into the catalog. The shape mirrors
// the helper rat-plugin-demo-loader uses — same endpoints, same idiom.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// CreateNamespace is idempotent — 409 from ratd is treated as success.
func (c *ratdClient) CreateNamespace(ctx context.Context, ns string) error {
	body, _ := json.Marshal(map[string]string{"name": ns})
	_, status, err := c.do(ctx, http.MethodPost, "/api/v1/namespaces", body)
	if err != nil {
		return err
	}
	if status == http.StatusConflict || (status >= 200 && status < 300) {
		return nil
	}
	return fmt.Errorf("create namespace %s: HTTP %d", ns, status)
}

type createPipelineResp struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Name      string `json:"name"`
	S3Path    string `json:"s3_path"`
}

// CreatePipeline returns the s3_path either from a fresh create or — on
// 409 — a fabricated one matching ratd's layout so a follow-up PUT to
// /api/v1/files/{path} still lands the SQL.
func (c *ratdClient) CreatePipeline(
	ctx context.Context, ns, layer, name, ptype, description string,
) (*createPipelineResp, error) {
	body, _ := json.Marshal(map[string]string{
		"namespace": ns, "layer": layer, "name": name,
		"type": ptype, "description": description,
	})
	raw, status, err := c.do(ctx, http.MethodPost, "/api/v1/pipelines", body)
	if err != nil {
		return nil, err
	}
	if status == http.StatusConflict {
		return &createPipelineResp{
			Namespace: ns, Layer: layer, Name: name,
			S3Path: fmt.Sprintf("%s/pipelines/%s/%s/", ns, layer, name),
		}, nil
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("create pipeline %s.%s.%s: HTTP %d: %s",
			ns, layer, name, status, string(raw))
	}
	var resp createPipelineResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode pipeline create: %w", err)
	}
	return &resp, nil
}

func (c *ratdClient) WriteFile(ctx context.Context, path, content string) error {
	body, _ := json.Marshal(map[string]string{"content": content})
	raw, status, err := c.do(ctx, http.MethodPut, "/api/v1/files/"+path, body)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("write file %s: HTTP %d: %s", path, status, string(raw))
}

// DeletePipeline removes a pipeline (and its s3 files) from ratd. We
// treat 404 as success so reapplying a teardown is safe.
func (c *ratdClient) DeletePipeline(ctx context.Context, ns, layer, name string) error {
	raw, status, err := c.do(ctx, http.MethodDelete,
		fmt.Sprintf("/api/v1/pipelines/%s/%s/%s", ns, layer, name), nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || (status >= 200 && status < 300) {
		return nil
	}
	return fmt.Errorf("delete pipeline: HTTP %d: %s", status, string(raw))
}

type scheduleEntry struct {
	ID         string `json:"id"`
	PipelineID string `json:"pipeline_id"`
}

// CreateSchedule attaches a cron schedule to the pipeline; the platform
// scheduler picks it up on the next tick — no restart needed. Returns
// the new schedule ID (we need it for later updates and tear-down).
func (c *ratdClient) CreateSchedule(
	ctx context.Context, ns, layer, pipeline, cron string, enabled bool,
) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"namespace": ns, "layer": layer, "pipeline": pipeline,
		"cron": cron, "enabled": enabled,
	})
	raw, status, err := c.do(ctx, http.MethodPost, "/api/v1/schedules", body)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("create schedule: HTTP %d: %s", status, string(raw))
	}
	var resp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &resp)
	return resp.ID, nil
}

// FindSchedulesFor returns every schedule attached to a given pipeline.
// ratd's /schedules list keys by pipeline_id (UUID), not the
// namespace/layer/name triple — so we first GET the pipeline to learn
// its id, then filter the schedule list locally. Typically one schedule
// matches, but we return all so a tear-down catches duplicates.
func (c *ratdClient) FindSchedulesFor(ctx context.Context, ns, layer, pipeline string) ([]scheduleEntry, error) {
	pipelineID, err := c.getPipelineID(ctx, ns, layer, pipeline)
	if err != nil {
		return nil, err
	}
	if pipelineID == "" {
		// Pipeline already gone — no schedules to clean up.
		return nil, nil
	}
	raw, status, err := c.do(ctx, http.MethodGet, "/api/v1/schedules", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("list schedules: HTTP %d", status)
	}
	// ratd returns either an array or {schedules:[]} depending on age —
	// handle both shapes so we don't break across versions.
	var arr []scheduleEntry
	if err := json.Unmarshal(raw, &arr); err != nil {
		var wrapped struct {
			Schedules []scheduleEntry `json:"schedules"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 != nil {
			return nil, fmt.Errorf("decode schedules: %w", err)
		}
		arr = wrapped.Schedules
	}
	out := make([]scheduleEntry, 0)
	for _, s := range arr {
		if s.PipelineID == pipelineID {
			out = append(out, s)
		}
	}
	return out, nil
}

// getPipelineID returns the UUID of a pipeline by its (ns, layer, name)
// triple, or "" if it doesn't exist (404).
func (c *ratdClient) getPipelineID(ctx context.Context, ns, layer, name string) (string, error) {
	raw, status, err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/api/v1/pipelines/%s/%s/%s", ns, layer, name), nil)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", nil
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("get pipeline %s/%s/%s: HTTP %d", ns, layer, name, status)
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode pipeline: %w", err)
	}
	return resp.ID, nil
}

func (c *ratdClient) DeleteSchedule(ctx context.Context, id string) error {
	raw, status, err := c.do(ctx, http.MethodDelete, "/api/v1/schedules/"+id, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || (status >= 200 && status < 300) {
		return nil
	}
	return fmt.Errorf("delete schedule %s: HTTP %d: %s", id, status, string(raw))
}

// SubmitRun triggers an immediate run of a pipeline (used for the
// "sync now" button). Returns the new run_id so callers can poll it.
func (c *ratdClient) SubmitRun(ctx context.Context, ns, layer, name, trigger string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"namespace": ns, "layer": layer, "pipeline": name, "trigger": trigger,
	})
	raw, status, err := c.do(ctx, http.MethodPost, "/api/v1/runs", body)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("submit run: HTTP %d: %s", status, string(raw))
	}
	var resp struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(raw, &resp)
	return resp.RunID, nil
}

func (c *ratdClient) do(
	ctx context.Context, method, path string, body []byte,
) ([]byte, int, error) {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	return raw, resp.StatusCode, nil
}
