package main

// In-memory state for the pg-sync plugin. State has two collections:
//   - connections: a named pointer to a secret (which holds the actual
//     postgresql:// URL) plus an optional description.
//   - tables: per-table sync definitions (source → target, mode, cron).
//
// The whole state is persisted to ratd as the plugin's config so a
// container restart picks up where we left off.

import (
	"errors"
	"sort"
	"sync"
	"time"
)

type SyncMode string

const (
	ModeSnapshot    SyncMode = "snapshot"    // full refresh every run
	ModeIncremental SyncMode = "incremental" // watermark-filtered append
)

// Connection points at a postgresql:// URL stored in the secrets plugin.
// We never copy the URL into our state — it stays in the secrets vault
// and we resolve it on the fly when we generate or regenerate pipelines.
type Connection struct {
	Name        string    `json:"name"`
	SecretName  string    `json:"secret_name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableSync describes one external pg table mirrored into Iceberg.
// Each row corresponds to exactly one generated SQL pipeline + schedule
// in ratd; the IDs match so we can find them again on update or delete.
type TableSync struct {
	ID              string    `json:"id"`
	Connection      string    `json:"connection"`
	SourceSchema    string    `json:"source_schema"`
	SourceTable     string    `json:"source_table"`
	TargetNamespace string    `json:"target_namespace"`
	TargetLayer     string    `json:"target_layer"`
	TargetName      string    `json:"target_name"`
	Mode            SyncMode  `json:"mode"`
	WatermarkColumn string    `json:"watermark_column,omitempty"`
	PrimaryKey      string    `json:"primary_key,omitempty"` // required for incremental — used as Iceberg unique_key
	Cron            string    `json:"cron"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastSyncedAt    *time.Time `json:"last_synced_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

// Validate is the single source of truth for what makes a sync row
// well-formed — the API and the rehydrator both go through it.
func (t *TableSync) Validate() error {
	if t.Connection == "" {
		return errors.New("connection is required")
	}
	if t.SourceTable == "" {
		return errors.New("source_table is required")
	}
	if t.TargetNamespace == "" {
		return errors.New("target_namespace is required")
	}
	if t.TargetLayer == "" {
		t.TargetLayer = "bronze"
	}
	if t.TargetName == "" {
		t.TargetName = t.SourceTable
	}
	if t.SourceSchema == "" {
		t.SourceSchema = "public"
	}
	if t.Cron == "" {
		t.Cron = defaultCron(t.Mode)
	}
	if t.Mode == "" {
		t.Mode = ModeSnapshot
	}
	if t.Mode == ModeIncremental {
		if t.WatermarkColumn == "" {
			return errors.New("watermark_column is required for incremental mode")
		}
		if t.PrimaryKey == "" {
			return errors.New("primary_key is required for incremental mode (used as the Iceberg unique_key for dedup)")
		}
	}
	if t.Mode != ModeSnapshot && t.Mode != ModeIncremental {
		return errors.New("mode must be snapshot or incremental")
	}
	return nil
}

// defaultCron picks a sensible cadence per mode.
//
//   - snapshot: every 5 minutes — full refresh is heavy.
//   - incremental: every 30 seconds — fast enough for "near-instant"
//     without overlap. The runner currently falls back to writing
//     directly to `main` if ephemeral-branch creation fails, and
//     overlapping runs on `main` can produce duplicate rows; 30s gives
//     each run room to finish before the next tick. Override per-table
//     for tighter cadence once branch-per-run is stabilised.
func defaultCron(m SyncMode) string {
	if m == ModeIncremental {
		return "*/30 * * * * *"
	}
	return "0 */5 * * * *"
}

type stateSnapshot struct {
	Connections []Connection `json:"connections"`
	Tables      []TableSync  `json:"tables"`
}

type store struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	tables      map[string]*TableSync
}

func newStore() *store {
	return &store{
		connections: map[string]*Connection{},
		tables:      map[string]*TableSync{},
	}
}

func (s *store) hydrate(snap stateSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections = map[string]*Connection{}
	for i := range snap.Connections {
		c := snap.Connections[i]
		s.connections[c.Name] = &c
	}
	s.tables = map[string]*TableSync{}
	for i := range snap.Tables {
		t := snap.Tables[i]
		s.tables[t.ID] = &t
	}
}

func (s *store) snapshotLocked() stateSnapshot {
	snap := stateSnapshot{
		Connections: make([]Connection, 0, len(s.connections)),
		Tables:      make([]TableSync, 0, len(s.tables)),
	}
	for _, c := range s.connections {
		snap.Connections = append(snap.Connections, *c)
	}
	for _, t := range s.tables {
		snap.Tables = append(snap.Tables, *t)
	}
	sort.Slice(snap.Connections, func(i, j int) bool { return snap.Connections[i].Name < snap.Connections[j].Name })
	sort.Slice(snap.Tables, func(i, j int) bool { return snap.Tables[i].ID < snap.Tables[j].ID })
	return snap
}

func (s *store) snapshot() stateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *store) listConnections() []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Connection, 0, len(s.connections))
	for _, c := range s.connections {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *store) getConnection(name string) (*Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.connections[name]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

func (s *store) upsertConnection(c Connection) (*Connection, stateSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := s.connections[c.Name]; ok {
		c.CreatedAt = existing.CreatedAt
	} else {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	stored := c
	s.connections[c.Name] = &stored
	return &stored, s.snapshotLocked()
}

func (s *store) deleteConnection(name string) (bool, stateSnapshot, []TableSync) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.connections[name]; !ok {
		return false, stateSnapshot{}, nil
	}
	// Cascade: every table using this connection is now orphaned. Collect
	// them so the caller can tear down their pipelines + schedules.
	var orphaned []TableSync
	for id, t := range s.tables {
		if t.Connection == name {
			orphaned = append(orphaned, *t)
			delete(s.tables, id)
		}
	}
	delete(s.connections, name)
	return true, s.snapshotLocked(), orphaned
}

func (s *store) listTables() []TableSync {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TableSync, 0, len(s.tables))
	for _, t := range s.tables {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Connection != out[j].Connection {
			return out[i].Connection < out[j].Connection
		}
		return out[i].SourceTable < out[j].SourceTable
	})
	return out
}

func (s *store) getTable(id string) (*TableSync, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tables[id]
	if !ok {
		return nil, false
	}
	cp := *t
	return &cp, true
}

func (s *store) upsertTable(t TableSync) (*TableSync, stateSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := s.tables[t.ID]; ok {
		t.CreatedAt = existing.CreatedAt
		t.LastSyncedAt = existing.LastSyncedAt
		t.LastError = existing.LastError
	} else {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	stored := t
	s.tables[t.ID] = &stored
	return &stored, s.snapshotLocked()
}

func (s *store) deleteTable(id string) (*TableSync, stateSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tables[id]
	if !ok {
		return nil, stateSnapshot{}, false
	}
	deleted := *t
	delete(s.tables, id)
	return &deleted, s.snapshotLocked(), true
}

func (s *store) markSynced(id string, when time.Time, errMsg string) (stateSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tables[id]
	if !ok {
		return stateSnapshot{}, false
	}
	w := when
	t.LastSyncedAt = &w
	t.LastError = errMsg
	t.UpdatedAt = time.Now().UTC()
	return s.snapshotLocked(), true
}
