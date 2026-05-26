package main

// Event ring buffer + per-source fingerprint snapshots. The poller fills
// snapshots; the differ derives events from snapshot A → snapshot B.

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
)

type EventKind string

const (
	EvtPluginRegistered    EventKind = "plugin.registered"
	EvtPluginUnregistered  EventKind = "plugin.unregistered"
	EvtPluginConfigChanged EventKind = "plugin.config_changed"
	EvtPluginHealthChanged EventKind = "plugin.health_changed"
	EvtPipelineCreated     EventKind = "pipeline.created"
	EvtPipelineDeleted     EventKind = "pipeline.deleted"
	EvtScheduleCreated     EventKind = "schedule.created"
	EvtScheduleDeleted     EventKind = "schedule.deleted"
	EvtScheduleToggled     EventKind = "schedule.toggled"
	EvtSecretCreated       EventKind = "secret.created"
	EvtSecretDeleted       EventKind = "secret.deleted"
	EvtSecretRotated       EventKind = "secret.rotated"
	EvtRunCompleted        EventKind = "run.completed"
	EvtNamespaceCreated    EventKind = "namespace.created"
	EvtNamespaceDeleted    EventKind = "namespace.deleted"
	EvtTableCreated        EventKind = "table.created"
	EvtTableDeleted        EventKind = "table.deleted"
	EvtTableRowsChanged    EventKind = "table.rows_changed"
)

// Event is one entry in the feed. Subject is the thing that changed
// (plugin name, pipeline ns.layer.name, secret name, run id, etc.).
// Before/After are arbitrary JSON snapshots for the drill-in viewer.
type Event struct {
	ID       string          `json:"id"`
	Time     time.Time       `json:"time"`
	Kind     EventKind       `json:"kind"`
	Subject  string          `json:"subject"`
	Summary  string          `json:"summary"`
	Before   json.RawMessage `json:"before,omitempty"`
	After    json.RawMessage `json:"after,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// snapshot is what we capture each poll tick. Maps keyed by stable id
// so deletion + addition detection is a single set-diff.
type snapshot struct {
	Time       time.Time           `json:"time"`
	Plugins    map[string]pluginFP `json:"plugins"`
	Pipelines  map[string]string   `json:"pipelines"`  // "ns/layer/name" → "type"
	Schedules  map[string]scheduleFP `json:"schedules"` // schedule id → fingerprint
	Secrets    map[string]string   `json:"secrets"`    // name → updated_at
	Namespaces map[string]bool     `json:"namespaces"`
	Tables     map[string]int64    `json:"tables"`     // "ns.layer.name" → row_count
	Runs       map[string]string   `json:"runs"`       // run id → status (only completed/failed are kept)
}

// pluginFP collapses the fields we care about. A content hash on the
// config JSON lets us cheap-compare for change without keeping the whole
// blob in every snapshot; we re-fetch the full doc when emitting the
// event so before/after JSON is accurate.
type pluginFP struct {
	Healthy    bool   `json:"healthy"`
	Version    string `json:"version"`
	Status     string `json:"status"`
	ConfigHash string `json:"config_hash"`
}

type scheduleFP struct {
	PipelineID string `json:"pipeline_id"`
	Cron       string `json:"cron"`
	Enabled    bool   `json:"enabled"`
}

// store holds the event ring buffer + the latest snapshot. The buffer
// lives in plugin config (PUT /api/v1/plugins/diff/config) so it
// survives restarts.
type store struct {
	mu       sync.RWMutex
	events   []Event
	maxEvents int
	last     *snapshot
}

func newStore(maxEvents int) *store {
	return &store{events: []Event{}, maxEvents: maxEvents}
}

func (s *store) hydrate(events []Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(events) > s.maxEvents {
		events = events[len(events)-s.maxEvents:]
	}
	s.events = events
}

func (s *store) snapshotEvents() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

func (s *store) appendEvents(evs []Event) []Event {
	if len(evs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evs...)
	if len(s.events) > s.maxEvents {
		s.events = s.events[len(s.events)-s.maxEvents:]
	}
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

// list returns events newest-first, optionally filtered by kind prefix
// (e.g. "plugin.") and capped to limit.
func (s *store) list(kindPrefix string, limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, 0, len(s.events))
	for i := len(s.events) - 1; i >= 0; i-- {
		e := s.events[i]
		if kindPrefix != "" && !startsWith(string(e.Kind), kindPrefix) {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *store) setLast(snap *snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = snap
}

func (s *store) getLast() *snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// sortKeys is a small helper used by the differ to walk maps in a
// deterministic order so events come out grouped sensibly.
func sortKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
