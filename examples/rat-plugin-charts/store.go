package main

import (
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ── Domain types ──────────────────────────────────────────────────
//
// A Chart is a *live* definition: only its SQL is stored, never its data.
// The query is re-run against ratd every time the chart is viewed, so every
// dashboard and report built from charts always reflects current data.

// Chart is a saved, live chart definition.
type Chart struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Type      string    `json:"type"` // bar | line | area | pie
	SQL       string    `json:"sql"`
	XColumn   string    `json:"x_column"`  // category / x-axis result column
	YColumns  []string  `json:"y_columns"` // numeric series result column(s)
	CreatedAt time.Time `json:"created_at"`
}

// Widget places a chart on a dashboard's modular grid.
type Widget struct {
	ChartID string `json:"chart_id"`
	Width   int    `json:"width"`  // grid columns spanned (1–4)
	Height  int    `json:"height"` // row height units (1–3)
}

// Dashboard is an ordered, modular collection of chart widgets.
type Dashboard struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Widgets   []Widget  `json:"widgets"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReportBlock is one section of a report: narrative markdown or a chart.
type ReportBlock struct {
	Kind    string `json:"kind"`               // "text" | "chart"
	Text    string `json:"text,omitempty"`     // markdown, when kind == "text"
	ChartID string `json:"chart_id,omitempty"` // when kind == "chart"
}

// Report is a narrative document interleaving text and live charts.
type Report struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Blocks    []ReportBlock `json:"blocks"`
	CreatedAt time.Time     `json:"created_at"`
}

// ── In-memory store ───────────────────────────────────────────────

// store is the in-memory catalogue of charts, dashboards and reports. It is
// safe for concurrent use. State is lost on restart — fine for an example
// plugin; a production build would back this with Postgres.
//
// Stored structs are treated as immutable once created: updates replace the
// whole struct, so a pointer handed to a reader is a stable snapshot.
type store struct {
	mu         sync.RWMutex
	charts     map[string]*Chart
	dashboards map[string]*Dashboard
	reports    map[string]*Report
	seq        atomic.Uint64
}

func newStore() *store {
	return &store{
		charts:     map[string]*Chart{},
		dashboards: map[string]*Dashboard{},
		reports:    map[string]*Report{},
	}
}

// id returns a short, unique identifier with the given prefix. The atomic
// counter guarantees uniqueness even within the same nanosecond.
func (s *store) id(prefix string) string {
	n := s.seq.Add(1)
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) +
		"-" + strconv.FormatUint(n, 36)
}

// ── Charts ────────────────────────────────────────────────────────

func (s *store) createChart(c *Chart) *Chart {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.ID = s.id("chart")
	c.CreatedAt = time.Now().UTC()
	s.charts[c.ID] = c
	return c
}

func (s *store) getChart(id string) (*Chart, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.charts[id]
	return c, ok
}

func (s *store) listCharts() []*Chart {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Chart, 0, len(s.charts))
	for _, c := range s.charts {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *store) deleteChart(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.charts[id]; !ok {
		return false
	}
	delete(s.charts, id)
	return true
}

// ── Dashboards ────────────────────────────────────────────────────

func (s *store) createDashboard(d *Dashboard) *Dashboard {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.ID = s.id("dash")
	now := time.Now().UTC()
	d.CreatedAt, d.UpdatedAt = now, now
	if d.Widgets == nil {
		d.Widgets = []Widget{}
	}
	s.dashboards[d.ID] = d
	return d
}

func (s *store) getDashboard(id string) (*Dashboard, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.dashboards[id]
	return d, ok
}

func (s *store) listDashboards() []*Dashboard {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Dashboard, 0, len(s.dashboards))
	for _, d := range s.dashboards {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// updateDashboard replaces a dashboard's title and/or widgets, returning a
// fresh snapshot. A nil field is left unchanged.
func (s *store) updateDashboard(id string, title *string, widgets *[]Widget) (*Dashboard, bool) {
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
	if widgets != nil {
		next.Widgets = *widgets
	}
	next.UpdatedAt = time.Now().UTC()
	s.dashboards[id] = &next
	return &next, true
}

func (s *store) deleteDashboard(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dashboards[id]; !ok {
		return false
	}
	delete(s.dashboards, id)
	return true
}

// ── Reports ───────────────────────────────────────────────────────

func (s *store) createReport(rep *Report) *Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep.ID = s.id("report")
	rep.CreatedAt = time.Now().UTC()
	if rep.Blocks == nil {
		rep.Blocks = []ReportBlock{}
	}
	s.reports[rep.ID] = rep
	return rep
}

func (s *store) getReport(id string) (*Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rep, ok := s.reports[id]
	return rep, ok
}

func (s *store) listReports() []*Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Report, 0, len(s.reports))
	for _, rep := range s.reports {
		out = append(out, rep)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *store) deleteReport(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.reports[id]; !ok {
		return false
	}
	delete(s.reports, id)
	return true
}
