package main

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

// ratdClient wraps the ratd HTTP API calls the installer makes: create
// namespaces, create pipelines, write pipeline files, create quality tests,
// submit runs. Errors that already-exist (HTTP 409) are reported as conflicts
// so the installer can treat the demo as idempotent.
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

type conflictErr struct{ msg string }

func (e *conflictErr) Error() string { return e.msg }

func isConflict(err error) bool { _, ok := err.(*conflictErr); return ok }

// CreateNamespace POSTs /api/v1/namespaces with {"name": ns}.
func (c *ratdClient) CreateNamespace(ctx context.Context, ns string) error {
	body, _ := json.Marshal(map[string]string{"name": ns})
	_, status, err := c.do(ctx, http.MethodPost, "/api/v1/namespaces", body)
	if err != nil {
		return err
	}
	return statusError("create namespace "+ns, status)
}

// CreatePipelineResponse mirrors the fields the installer needs.
type CreatePipelineResponse struct {
	Namespace    string   `json:"namespace"`
	Layer        string   `json:"layer"`
	Name         string   `json:"name"`
	S3Path       string   `json:"s3_path"`
	FilesCreated []string `json:"files_created"`
}

// CreatePipeline POSTs /api/v1/pipelines and returns the s3_path used for
// the pipeline's files.
func (c *ratdClient) CreatePipeline(
	ctx context.Context, ns, layer, name, ptype, description string,
) (*CreatePipelineResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"namespace": ns, "layer": layer, "name": name,
		"type": ptype, "description": description,
	})
	raw, status, err := c.do(ctx, http.MethodPost, "/api/v1/pipelines", body)
	if err != nil {
		return nil, err
	}
	if status == http.StatusConflict {
		// Already created — fabricate a plausible s3_path so the caller can
		// still write the pipeline file. The file write is a PUT, which is
		// safe to repeat.
		return &CreatePipelineResponse{
			Namespace: ns, Layer: layer, Name: name,
			S3Path: fmt.Sprintf("%s/pipelines/%s/%s/", ns, layer, name),
		}, &conflictErr{fmt.Sprintf("pipeline %s.%s.%s already exists", ns, layer, name)}
	}
	if err := statusError("create pipeline", status); err != nil {
		return nil, err
	}
	var resp CreatePipelineResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode create-pipeline response: %w", err)
	}
	return &resp, nil
}

// WriteFile puts a file at the given S3 path using PUT /api/v1/files/{path}.
func (c *ratdClient) WriteFile(ctx context.Context, path, content string) error {
	body, _ := json.Marshal(map[string]string{"content": content})
	_, status, err := c.do(ctx, http.MethodPut, "/api/v1/files/"+path, body)
	if err != nil {
		return err
	}
	return statusError("write file "+path, status)
}

// CreateQualityTest POSTs the quality endpoint nested under the pipeline.
func (c *ratdClient) CreateQualityTest(
	ctx context.Context, ns, layer, pipeline, name, sql, severity, description string,
) error {
	body, _ := json.Marshal(map[string]string{
		"name": name, "sql": sql, "severity": severity, "description": description,
	})
	_, status, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/pipelines/%s/%s/%s/tests", ns, layer, pipeline), body)
	if err != nil {
		return err
	}
	return statusError("create quality test "+name, status)
}

// SubmitRun POSTs /api/v1/runs to trigger a pipeline run. The ratd handler
// expects the field name "pipeline" (not "pipeline_name"). Returns the
// resulting run_id so the caller can poll it (e.g. RunAll waits for each
// layer to complete before triggering the next).
func (c *ratdClient) SubmitRun(ctx context.Context, ns, layer, name, trigger string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"namespace": ns, "layer": layer, "pipeline": name, "trigger": trigger,
	})
	raw, status, err := c.do(ctx, http.MethodPost, "/api/v1/runs", body)
	if err != nil {
		return "", err
	}
	if err := statusError("submit run for "+name, status); err != nil {
		return "", err
	}
	var resp struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(raw, &resp)
	return resp.RunID, nil
}

// GetRunStatus polls GET /api/v1/runs/{id} and returns the current status
// (pending / running / success / failed / cancelled). Used by RunAll to wait
// for a layer to finish before triggering the next.
func (c *ratdClient) GetRunStatus(ctx context.Context, id string) (string, error) {
	raw, status, err := c.do(ctx, http.MethodGet, "/api/v1/runs/"+id, nil)
	if err != nil {
		return "", err
	}
	if err := statusError("get run "+id, status); err != nil {
		return "", err
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.Status, nil
}

// SetTableMetadata PUTs the table's description + column descriptions —
// this is the same endpoint the docs-assistant plugin writes to.
func (c *ratdClient) SetTableMetadata(
	ctx context.Context, ns, layer, name, description string, columns map[string]string,
) error {
	body, _ := json.Marshal(map[string]any{
		"description":         description,
		"column_descriptions": columns,
	})
	_, status, err := c.do(ctx, http.MethodPut,
		fmt.Sprintf("/api/v1/tables/%s/%s/%s/metadata", ns, layer, name), body)
	if err != nil {
		return err
	}
	return statusError("set metadata for "+name, status)
}

// CreateSchedule POSTs /api/v1/schedules to attach a cron schedule to a
// pipeline. ratd's scheduler picks it up automatically — no restart needed.
func (c *ratdClient) CreateSchedule(
	ctx context.Context, ns, layer, pipeline, cron string, enabled bool,
) error {
	body, _ := json.Marshal(map[string]any{
		"namespace": ns, "layer": layer, "pipeline": pipeline,
		"cron": cron, "enabled": enabled,
	})
	_, status, err := c.do(ctx, http.MethodPost, "/api/v1/schedules", body)
	if err != nil {
		return err
	}
	return statusError("create schedule for "+pipeline, status)
}

// statusError turns a non-2xx status into an error — except 409 which becomes
// a conflictErr so the installer can treat it as idempotent.
func statusError(action string, status int) error {
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusConflict {
		return &conflictErr{action + ": already exists"}
	}
	return fmt.Errorf("%s: HTTP %d", action, status)
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
