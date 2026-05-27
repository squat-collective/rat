package main

import (
	"context"
	"fmt"
	"time"
)

// RunAllResult is what /run returns to the panel: per-layer counts and any
// errors that surfaced along the way.
type RunAllResult struct {
	DemoID    string         `json:"demo_id"`
	Namespace string         `json:"namespace"`
	Layers    []LayerOutcome `json:"layers"`
	Errors    []string       `json:"errors,omitempty"`
}

type LayerOutcome struct {
	Layer      string   `json:"layer"`
	Submitted  []string `json:"submitted"`   // pipeline names submitted
	Succeeded  []string `json:"succeeded"`
	Failed     []string `json:"failed"`
	DurationMS int64    `json:"duration_ms"`
}

// RunAll submits every pipeline in dependency order (bronze → silver → gold),
// staggers within a layer to keep the runner happy, and polls each layer to
// completion before triggering the next. The whole call blocks until every
// run is in a terminal state (success / failed / cancelled) or the per-layer
// timeout fires.
func (i *Installer) RunAll(ctx context.Context, m *Manifest, nsOverride string) *RunAllResult {
	res := &RunAllResult{DemoID: m.ID, Namespace: m.Namespace}
	if nsOverride != "" {
		res.Namespace = nsOverride
	}
	ns := res.Namespace

	for _, layer := range []string{"bronze", "silver", "gold"} {
		layerStart := time.Now()
		outcome := LayerOutcome{Layer: layer}

		// Pipelines in this layer, preserving manifest order.
		pipes := pipelinesByLayer(m, layer)
		if len(pipes) == 0 {
			continue
		}

		// Submit, staggered to keep the runner's DuckDB happy.
		type pending struct {
			pipeline string
			runID    string
		}
		var inflight []pending
		for idx, p := range pipes {
			if idx > 0 {
				select {
				case <-ctx.Done():
					res.Errors = append(res.Errors, "cancelled")
					return res
				case <-time.After(3 * time.Second):
				}
			}
			runID, err := i.ratd.SubmitRun(ctx, ns, p.Layer, p.Name, "manual")
			if err != nil {
				outcome.Failed = append(outcome.Failed, p.Name)
				res.Errors = append(res.Errors, fmt.Sprintf("submit %s.%s: %s", p.Layer, p.Name, err))
				continue
			}
			outcome.Submitted = append(outcome.Submitted, p.Name)
			inflight = append(inflight, pending{pipeline: p.Name, runID: runID})
		}

		// Wait for every in-flight run to reach a terminal status before
		// moving to the next layer — silver/gold need their upstream tables
		// fully materialised.
		deadline := time.Now().Add(120 * time.Second)
		for _, pp := range inflight {
			final := waitForRun(ctx, i.ratd, pp.runID, deadline)
			switch final {
			case "success":
				outcome.Succeeded = append(outcome.Succeeded, pp.pipeline)
			case "":
				outcome.Failed = append(outcome.Failed, pp.pipeline)
				res.Errors = append(res.Errors, fmt.Sprintf("%s.%s: timed out waiting for terminal status", layer, pp.pipeline))
			default:
				// "failed" / "cancelled" / anything else
				outcome.Failed = append(outcome.Failed, pp.pipeline)
				res.Errors = append(res.Errors, fmt.Sprintf("%s.%s: %s", layer, pp.pipeline, final))
			}
		}

		outcome.DurationMS = time.Since(layerStart).Milliseconds()
		res.Layers = append(res.Layers, outcome)

		// Don't trigger the next layer if no upstream succeeded — silver/gold
		// would just fail with NoSuchTable.
		if len(outcome.Succeeded) == 0 {
			res.Errors = append(res.Errors,
				fmt.Sprintf("layer '%s' produced no successful runs — aborting", layer))
			return res
		}
	}

	return res
}

func pipelinesByLayer(m *Manifest, layer string) []ManifestPipe {
	out := make([]ManifestPipe, 0, len(m.Pipelines))
	for _, p := range m.Pipelines {
		if p.Layer == layer {
			out = append(out, p)
		}
	}
	return out
}

// waitForRun polls a run's status until it is terminal, the context is
// cancelled, or the deadline passes. Returns the final status or "" on timeout.
func waitForRun(ctx context.Context, ratd *ratdClient, runID string, deadline time.Time) string {
	const interval = 2 * time.Second
	for {
		if time.Now().After(deadline) {
			return ""
		}
		status, err := ratd.GetRunStatus(ctx, runID)
		if err == nil {
			switch status {
			case "success", "failed", "cancelled":
				return status
			}
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(interval):
		}
	}
}
