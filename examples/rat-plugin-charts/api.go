package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// api serves the dashboards REST API. ratd proxies it at /api/v1/x/charts/* —
// the proxy forwards the whole path suffix, so every route below is reachable.
type api struct {
	store *store
	ratd  *ratdClient
}

func newAPI(s *store, rc *ratdClient) *api {
	return &api{store: s, ratd: rc}
}

// componentTypes are the component kinds a dashboard accepts. Their Props
// shapes live in the portal UI — the Go side only checks the type name.
var componentTypes = map[string]bool{
	"chart": true, "heading": true, "markdown": true, "metric": true, "ai": true,
}

// mux wires every REST route. Go 1.22+ ServeMux gives method + {id} patterns.
func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()

	m.HandleFunc("POST /dashboards", a.createDashboard)
	m.HandleFunc("GET /dashboards", a.listDashboards)
	m.HandleFunc("GET /dashboards/{id}", a.getDashboard)
	m.HandleFunc("PATCH /dashboards/{id}", a.updateDashboard)
	m.HandleFunc("DELETE /dashboards/{id}", a.deleteDashboard)
	m.HandleFunc("POST /dashboards/{id}/components", a.addComponent)

	// query runs ad-hoc read-only SQL — chart/metric components fetch their
	// data with it, and the chart editor previews with it.
	m.HandleFunc("POST /query", a.query)

	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// ── Dashboards ────────────────────────────────────────────────────

func (a *api) createDashboard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	writeJSON(w, http.StatusCreated, a.store.create(&Dashboard{Title: in.Title}))
}

func (a *api) listDashboards(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.list())
}

func (a *api) getDashboard(w http.ResponseWriter, r *http.Request) {
	d, ok := a.store.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// updateDashboard replaces a dashboard's title and/or its whole component list
// — the editor sends the full grid back after any change.
func (a *api) updateDashboard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title      *string      `json:"title"`
		Components *[]Component `json:"components"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
	}
	if in.Components != nil {
		norm := make([]Component, 0, len(*in.Components))
		for _, c := range *in.Components {
			if !componentTypes[c.Type] {
				writeErr(w, http.StatusBadRequest, "unknown component type: "+c.Type)
				return
			}
			norm = append(norm, a.normalizeComponent(c, 0))
		}
		in.Components = &norm
	}
	d, ok := a.store.update(r.PathValue("id"), in.Title, in.Components)
	if !ok {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *api) deleteDashboard(w http.ResponseWriter, r *http.Request) {
	if !a.store.delete(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// addComponent appends one component to a dashboard, placed on a fresh row at
// the bottom of the grid. Used by the chat's "pin to dashboard" action.
func (a *api) addComponent(w http.ResponseWriter, r *http.Request) {
	var c Component
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !componentTypes[c.Type] {
		writeErr(w, http.StatusBadRequest, "unknown component type: "+c.Type)
		return
	}
	d, ok := a.store.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "dashboard not found")
		return
	}
	bottom := 0
	for _, ex := range d.Components {
		if y := ex.Layout.Y + ex.Layout.H; y > bottom {
			bottom = y
		}
	}
	next := append(append([]Component{}, d.Components...), a.normalizeComponent(c, bottom))
	updated, _ := a.store.update(d.ID, nil, &next)
	writeJSON(w, http.StatusOK, updated)
}

// ── Query ─────────────────────────────────────────────────────────

func (a *api) query(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SQL string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.SQL) == "" {
		writeErr(w, http.StatusBadRequest, "sql is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, a.ratd.run(ctx, in.SQL))
}

// ── Helpers ───────────────────────────────────────────────────────

// normalizeComponent assigns a missing id, clamps the layout to the 12-column
// grid, and applies a sensible default layout for a zero value. fallbackY
// places a freshly-added component below the existing grid.
func (a *api) normalizeComponent(c Component, fallbackY int) Component {
	if strings.TrimSpace(c.ID) == "" {
		c.ID = a.store.id("cmp")
	}
	def := defaultLayout(c.Type)
	if c.Layout.W == 0 {
		c.Layout.W = def.W
	}
	if c.Layout.H == 0 {
		c.Layout.H = def.H
		c.Layout.Y = fallbackY
	}
	c.Layout.W = clamp(c.Layout.W, 1, 12)
	c.Layout.H = clamp(c.Layout.H, 1, 40)
	c.Layout.X = clamp(c.Layout.X, 0, 12-c.Layout.W)
	if c.Layout.Y < 0 {
		c.Layout.Y = 0
	}
	if len(c.Props) == 0 {
		c.Props = json.RawMessage("{}")
	}
	return c
}

// defaultLayout is the starting size for a freshly-added component of a type.
func defaultLayout(t string) Layout {
	switch t {
	case "heading":
		return Layout{W: 12, H: 2}
	case "markdown":
		return Layout{W: 6, H: 6}
	case "metric":
		return Layout{W: 3, H: 5}
	default: // chart, ai
		return Layout{W: 6, H: 9}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
