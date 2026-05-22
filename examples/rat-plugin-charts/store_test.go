package main

import (
	"encoding/json"
	"testing"
)

func TestDashboardCRUD(t *testing.T) {
	s := newStore()
	d := s.create(&Dashboard{Title: "Ops"})
	if d.ID == "" || d.CreatedAt.IsZero() {
		t.Fatal("created dashboard should have an ID and a timestamp")
	}
	if d.Components == nil {
		t.Error("a new dashboard should have a non-nil components slice")
	}
	got, ok := s.get(d.ID)
	if !ok || got.Title != "Ops" {
		t.Fatalf("get returned %v, %v", got, ok)
	}
	if len(s.list()) != 1 {
		t.Errorf("expected 1 dashboard, got %d", len(s.list()))
	}
	if !s.delete(d.ID) {
		t.Error("delete should report success")
	}
	if _, ok := s.get(d.ID); ok {
		t.Error("dashboard should be gone after delete")
	}
	if s.delete("missing") {
		t.Error("deleting a missing dashboard should report failure")
	}
}

func TestDashboardUpdate(t *testing.T) {
	s := newStore()
	d := s.create(&Dashboard{Title: "Ops"})

	// Rename only — components must be left untouched.
	title := "Ops v2"
	upd, ok := s.update(d.ID, &title, nil)
	if !ok || upd.Title != "Ops v2" {
		t.Fatalf("rename failed: %v, %v", upd, ok)
	}

	// Replace components.
	comps := []Component{{ID: "cmp-1", Type: "heading", Props: json.RawMessage(`{"text":"Hi"}`)}}
	upd, ok = s.update(d.ID, nil, &comps)
	if !ok || len(upd.Components) != 1 {
		t.Fatalf("component replace failed: %v", upd)
	}
	if upd.Title != "Ops v2" {
		t.Error("title should be preserved when only components are updated")
	}

	if _, ok := s.update("missing", &title, nil); ok {
		t.Error("updating a missing dashboard should report failure")
	}
}

func TestIDsAreUnique(t *testing.T) {
	s := newStore()
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := s.id("cmp")
		if seen[id] {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = true
	}
}
