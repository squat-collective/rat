package main

// differ.go derives Event objects from snapshot deltas.

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func diffSnapshots(prev, curr *snapshot, prevRaw, currRaw *rawSnapshot) []Event {
	var out []Event
	now := curr.Time

	// ── plugins ────────────────────────────────────────────────────
	for _, name := range sortKeys(curr.Plugins) {
		newFP := curr.Plugins[name]
		oldFP, existed := prev.Plugins[name]
		if !existed {
			out = append(out, makeEvent(now, EvtPluginRegistered, name,
				fmt.Sprintf("plugin %q registered (v%s)", name, newFP.Version),
				nil, currRaw.plugins[name]))
			continue
		}
		if oldFP.Healthy != newFP.Healthy {
			out = append(out, makeEvent(now, EvtPluginHealthChanged, name,
				fmt.Sprintf("plugin %q is now %s", name, healthLabel(newFP.Healthy)),
				map[string]any{"healthy": oldFP.Healthy},
				map[string]any{"healthy": newFP.Healthy}))
		}
		if oldFP.ConfigHash != newFP.ConfigHash {
			before := json.RawMessage("null")
			if prevRaw != nil {
				if p, ok := prevRaw.plugins[name]; ok && len(p.Config) > 0 {
					before = p.Config
				}
			}
			after := json.RawMessage("null")
			if p, ok := currRaw.plugins[name]; ok && len(p.Config) > 0 {
				after = p.Config
			}
			out = append(out, Event{
				ID: uuid.NewString(), Time: now, Kind: EvtPluginConfigChanged,
				Subject: name,
				Summary: fmt.Sprintf("plugin %q config changed", name),
				Before:  before, After: after,
			})
		}
	}
	for _, name := range sortKeys(prev.Plugins) {
		if _, still := curr.Plugins[name]; !still {
			out = append(out, makeEvent(now, EvtPluginUnregistered, name,
				fmt.Sprintf("plugin %q unregistered", name), nil, nil))
		}
	}

	// ── pipelines ──────────────────────────────────────────────────
	for _, k := range sortKeys(curr.Pipelines) {
		if _, existed := prev.Pipelines[k]; !existed {
			out = append(out, makeEvent(now, EvtPipelineCreated, k,
				fmt.Sprintf("pipeline %s created (%s)", k, curr.Pipelines[k]), nil, nil))
		}
	}
	for _, k := range sortKeys(prev.Pipelines) {
		if _, still := curr.Pipelines[k]; !still {
			out = append(out, makeEvent(now, EvtPipelineDeleted, k,
				fmt.Sprintf("pipeline %s deleted", k), nil, nil))
		}
	}

	// ── schedules ──────────────────────────────────────────────────
	for _, id := range sortKeys(curr.Schedules) {
		newS := curr.Schedules[id]
		oldS, existed := prev.Schedules[id]
		if !existed {
			out = append(out, makeEvent(now, EvtScheduleCreated, id,
				fmt.Sprintf("schedule created — cron %q, enabled=%v", newS.Cron, newS.Enabled),
				nil, currRaw.schedules[id]))
			continue
		}
		if oldS.Enabled != newS.Enabled {
			out = append(out, makeEvent(now, EvtScheduleToggled, id,
				fmt.Sprintf("schedule %s → %s", id[:8], enabledLabel(newS.Enabled)),
				map[string]any{"enabled": oldS.Enabled},
				map[string]any{"enabled": newS.Enabled}))
		}
	}
	for _, id := range sortKeys(prev.Schedules) {
		if _, still := curr.Schedules[id]; !still {
			out = append(out, makeEvent(now, EvtScheduleDeleted, id,
				fmt.Sprintf("schedule %s deleted", id[:8]), nil, nil))
		}
	}

	// ── secrets ────────────────────────────────────────────────────
	for _, name := range sortKeys(curr.Secrets) {
		newTS := curr.Secrets[name]
		oldTS, existed := prev.Secrets[name]
		if !existed {
			out = append(out, makeEvent(now, EvtSecretCreated, name,
				fmt.Sprintf("secret %q created", name), nil, nil))
			continue
		}
		if oldTS != newTS {
			out = append(out, makeEvent(now, EvtSecretRotated, name,
				fmt.Sprintf("secret %q rotated", name), nil, nil))
		}
	}
	for _, name := range sortKeys(prev.Secrets) {
		if _, still := curr.Secrets[name]; !still {
			out = append(out, makeEvent(now, EvtSecretDeleted, name,
				fmt.Sprintf("secret %q deleted", name), nil, nil))
		}
	}

	// ── namespaces ─────────────────────────────────────────────────
	for _, n := range sortKeys(curr.Namespaces) {
		if _, existed := prev.Namespaces[n]; !existed {
			out = append(out, makeEvent(now, EvtNamespaceCreated, n,
				fmt.Sprintf("namespace %q created", n), nil, nil))
		}
	}
	for _, n := range sortKeys(prev.Namespaces) {
		if _, still := curr.Namespaces[n]; !still {
			out = append(out, makeEvent(now, EvtNamespaceDeleted, n,
				fmt.Sprintf("namespace %q deleted", n), nil, nil))
		}
	}

	// ── tables ─────────────────────────────────────────────────────
	for _, k := range sortKeys(curr.Tables) {
		newCount := curr.Tables[k]
		oldCount, existed := prev.Tables[k]
		if !existed {
			ev := makeEvent(now, EvtTableCreated, k,
				fmt.Sprintf("table %s created (%d rows)", k, newCount),
				nil, map[string]any{"row_count": newCount})
			ev.Metadata = map[string]any{"table": k, "row_count": newCount}
			out = append(out, ev)
			continue
		}
		// Suppress phantom +0 deltas (ratd's tables endpoint returns 0
		// when the catalog hasn't refreshed yet, then the real count
		// later; we don't want to emit those as "rows changed").
		if newCount != oldCount && newCount > 0 && oldCount > 0 {
			ev := makeEvent(now, EvtTableRowsChanged, k,
				fmt.Sprintf("table %s rows: %d → %d (%+d)", k, oldCount, newCount, newCount-oldCount),
				map[string]any{"row_count": oldCount},
				map[string]any{"row_count": newCount})
			ev.Metadata = map[string]any{"table": k, "delta": newCount - oldCount}
			out = append(out, ev)
		}
	}
	for _, k := range sortKeys(prev.Tables) {
		if _, still := curr.Tables[k]; !still {
			out = append(out, makeEvent(now, EvtTableDeleted, k,
				fmt.Sprintf("table %s deleted", k), nil, nil))
		}
	}

	// ── runs ───────────────────────────────────────────────────────
	for _, id := range sortKeys(curr.Runs) {
		if _, seen := prev.Runs[id]; seen {
			continue // already emitted on a previous tick
		}
		r := currRaw.runs[id]
		// Resolve the affected table from the pipeline_id so the UI can
		// surface a "diff rows" button on this event that deep-links to
		// the row-diff drill-in for that table.
		tableKey := currRaw.pipelinesByID[r.PipelineID]
		summary := fmt.Sprintf("run %s (%s) — %s, %d rows",
			id[:8], r.Trigger, r.Status, r.RowsWritten)
		if tableKey != "" {
			summary = fmt.Sprintf("run %s on %s — %s, %d rows",
				id[:8], tableKey, r.Status, r.RowsWritten)
		}
		ev := makeEvent(now, EvtRunCompleted, id, summary, nil, r)
		ev.Metadata = map[string]any{
			"pipeline_id":  r.PipelineID,
			"status":       r.Status,
			"rows_written": r.RowsWritten,
		}
		if tableKey != "" {
			ev.Metadata["table"] = tableKey
		}
		out = append(out, ev)
	}

	return out
}

// makeEvent is a tiny helper that handles before/after JSON marshalling
// and rolls in a fresh UUID.
func makeEvent(t time.Time, kind EventKind, subject, summary string, before, after any) Event {
	ev := Event{
		ID: uuid.NewString(), Time: t, Kind: kind,
		Subject: subject, Summary: summary,
	}
	if before != nil {
		if b, err := json.Marshal(before); err == nil {
			ev.Before = b
		}
	}
	if after != nil {
		if b, err := json.Marshal(after); err == nil {
			ev.After = b
		}
	}
	return ev
}

func healthLabel(b bool) string {
	if b {
		return "healthy"
	}
	return "unhealthy"
}
func enabledLabel(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
