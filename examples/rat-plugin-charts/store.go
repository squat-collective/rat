package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ── Domain types ──────────────────────────────────────────────────
//
// A dashboard is a scrollable grid of typed components. There is no global
// chart catalogue: a chart lives inside a dashboard as a "chart" component, or
// is drawn on the fly in the AI chat. A component's Props is type-specific JSON
// the portal UI owns — so new component types need no changes here.

// Layout places a component on the 12-column dashboard grid.
type Layout struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Component is one item on a dashboard.
type Component struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"` // chart | heading | markdown | metric | ai
	Layout Layout          `json:"layout"`
	Props  json.RawMessage `json:"props"`
}

// Dashboard is a scrollable grid of components.
type Dashboard struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Components []Component `json:"components"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// ── In-memory store ───────────────────────────────────────────────

// store is the in-memory catalogue of dashboards. It is safe for concurrent
// use. State is lost on restart — fine for an example plugin; a production
// build would back this with Postgres.
//
// Stored dashboards are treated as immutable once created: update replaces the
// whole struct, so a pointer handed to a reader is a stable snapshot.
type store struct {
	mu         sync.RWMutex
	dashboards map[string]*Dashboard
	seq        atomic.Uint64
}

func newStore() *store {
	return &store{dashboards: map[string]*Dashboard{}}
}

// id returns a short, unique identifier with the given prefix.
func (s *store) id(prefix string) string {
	n := s.seq.Add(1)
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) +
		"-" + strconv.FormatUint(n, 36)
}

func (s *store) create(d *Dashboard) *Dashboard {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.ID = s.id("dash")
	now := time.Now().UTC()
	d.CreatedAt, d.UpdatedAt = now, now
	if d.Components == nil {
		d.Components = []Component{}
	}
	s.dashboards[d.ID] = d
	return d
}

func (s *store) get(id string) (*Dashboard, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.dashboards[id]
	return d, ok
}

func (s *store) list() []*Dashboard {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Dashboard, 0, len(s.dashboards))
	for _, d := range s.dashboards {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// update replaces a dashboard's title and/or components, returning a fresh
// snapshot. A nil field is left unchanged.
func (s *store) update(id string, title *string, components *[]Component) (*Dashboard, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.dashboards[id]
	if !ok {
		return nil, false
	}
	next := *cur // copy
	if title != nil {
		next.Title = *title
	}
	if components != nil {
		next.Components = *components
	}
	next.UpdatedAt = time.Now().UTC()
	s.dashboards[id] = &next
	return &next, true
}

func (s *store) delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dashboards[id]; !ok {
		return false
	}
	delete(s.dashboards, id)
	return true
}
