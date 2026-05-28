package main

import "testing"

func TestCapabilityCRUD(t *testing.T) {
	s := newStore()
	c := s.register(&Capability{Name: "data.analyze", Provider: "ai", Method: "POST", Path: "/analyze"})
	if c.RegisteredAt.IsZero() {
		t.Error("register should set RegisteredAt")
	}
	if c.Consumers == nil {
		t.Error("Consumers should be a non-nil slice")
	}
	got, ok := s.get("data.analyze")
	if !ok || got.Provider != "ai" {
		t.Fatalf("get returned %v, %v", got, ok)
	}
	if len(s.list()) != 1 {
		t.Errorf("expected 1 capability, got %d", len(s.list()))
	}
	if !s.delete("data.analyze") {
		t.Error("delete should report success")
	}
	if _, ok := s.get("data.analyze"); ok {
		t.Error("capability should be gone after delete")
	}
	if s.delete("missing") {
		t.Error("deleting a missing capability should report failure")
	}
}

func TestRegisterReplacesByName(t *testing.T) {
	s := newStore()
	s.register(&Capability{Name: "x", Provider: "a"})
	s.register(&Capability{Name: "x", Provider: "b"})
	if len(s.list()) != 1 {
		t.Fatalf("re-registering a name should replace, not add — got %d", len(s.list()))
	}
	got, _ := s.get("x")
	if got.Provider != "b" {
		t.Errorf("expected the replacement provider 'b', got %q", got.Provider)
	}
}
