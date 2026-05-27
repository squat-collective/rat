package main

// fingerprint.go captures one cross-cutting snapshot of "what exists
// right now" — used by the poller to feed the differ.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// capture takes a full snapshot. Best-effort per source: a failure to
// reach (say) the secrets plugin doesn't kill the whole poll — that
// source's section is just left empty and the differ treats it as
// "no known state" → no events emitted.
func capture(ctx context.Context, c *ratdClient, selfName string) (*snapshot, *rawSnapshot) {
	snap := &snapshot{
		Time:       time.Now().UTC(),
		Plugins:    map[string]pluginFP{},
		Pipelines:  map[string]string{},
		Schedules:  map[string]scheduleFP{},
		Secrets:    map[string]string{},
		Namespaces: map[string]bool{},
		Tables:     map[string]int64{},
		Runs:       map[string]string{},
	}
	raw := &rawSnapshot{
		plugins:       map[string]pluginListItem{},
		schedules:     map[string]scheduleItem{},
		secrets:       map[string]secretItem{},
		runs:          map[string]runItem{},
		pipelinesByID: map[string]string{},
	}

	if plugs, err := c.ListPlugins(ctx); err == nil {
		for _, p := range plugs {
			if p.Name == selfName {
				continue // don't emit events about our own config flicker
			}
			snap.Plugins[p.Name] = pluginFP{
				Healthy:    p.Healthy,
				Version:    p.Version,
				Status:     p.Status,
				ConfigHash: hashBytes(p.Config),
			}
			raw.plugins[p.Name] = p
		}
	}

	if pls, err := c.ListPipelines(ctx); err == nil {
		for _, p := range pls {
			key := fmt.Sprintf("%s/%s/%s", p.Namespace, p.Layer, p.Name)
			snap.Pipelines[key] = p.Type
			if p.ID != "" {
				raw.pipelinesByID[p.ID] = fmt.Sprintf("%s.%s.%s", p.Namespace, p.Layer, p.Name)
			}
		}
	}

	if scheds, err := c.ListSchedules(ctx); err == nil {
		for _, s := range scheds {
			snap.Schedules[s.ID] = scheduleFP{
				PipelineID: s.PipelineID, Cron: s.Cron, Enabled: s.Enabled,
			}
			raw.schedules[s.ID] = s
		}
	}

	for _, s := range c.ListSecrets(ctx) {
		snap.Secrets[s.Name] = s.UpdatedAt.Format(time.RFC3339Nano)
		raw.secrets[s.Name] = s
	}

	if nss, err := c.ListNamespaces(ctx); err == nil {
		for _, n := range nss {
			snap.Namespaces[n.Name] = true
		}
	}

	if tbls, err := c.ListTables(ctx); err == nil {
		for _, t := range tbls {
			key := fmt.Sprintf("%s.%s.%s", t.Namespace, t.Layer, t.Name)
			snap.Tables[key] = t.RowCount
		}
	}

	if runs, err := c.ListRecentRuns(ctx, 50); err == nil {
		for _, r := range runs {
			// Only retain terminal-state runs in the fingerprint — others
			// will tick into terminal on a later poll. This lets us emit a
			// `run.completed` event once and only once per run.
			if r.Status == "success" || r.Status == "failed" || r.Status == "cancelled" {
				snap.Runs[r.ID] = r.Status
				raw.runs[r.ID] = r
			}
		}
	}

	return snap, raw
}

// rawSnapshot keeps the full source objects for the same time slice as
// `snapshot` so the differ can populate before/after JSON without doing
// a second HTTP round-trip. pipelinesByID lets us resolve a run's
// pipeline_id → "ns.layer.name" key when enriching run.completed events
// with the table they affected (used for the "diff rows" deep link).
type rawSnapshot struct {
	plugins       map[string]pluginListItem
	schedules     map[string]scheduleItem
	secrets       map[string]secretItem
	runs          map[string]runItem
	pipelinesByID map[string]string // pipeline_id → "ns.layer.name"
}

func hashBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:12]) // first 12 bytes is enough for change detection
}
