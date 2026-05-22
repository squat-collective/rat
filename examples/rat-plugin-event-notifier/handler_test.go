package main

import (
	"context"
	"testing"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

func newTestHandler() *Handler {
	cfg := newConfigStore("http://ratd:8080", "event-notifier",
		notifierConfig{MaxEvents: defaultMaxEvents})
	return newHandler("event-notifier", "http://event-notifier:50090/bundle.js", cfg)
}

func TestHealthCheckServing(t *testing.T) {
	resp, err := newTestHandler().HealthCheck(
		context.Background(), connect.NewRequest(&pluginv1.HealthCheckRequest{}),
	)
	if err != nil {
		t.Fatalf("HealthCheck error: %v", err)
	}
	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		t.Fatalf("expected STATUS_SERVING, got %v", resp.Msg.Status)
	}
}

func TestDescribeAdvertisesContract(t *testing.T) {
	resp, err := newTestHandler().Describe(
		context.Background(), connect.NewRequest(&pluginv1.DescribeRequest{}),
	)
	if err != nil {
		t.Fatalf("Describe error: %v", err)
	}
	d := resp.Msg
	if d.Name != "event-notifier" {
		t.Errorf("name = %q, want event-notifier", d.Name)
	}
	if len(d.EventSubscriptions) != 2 {
		t.Errorf("event_subscriptions = %v, want 2 entries", d.EventSubscriptions)
	}
	if d.Ui == nil || d.Ui.BundleUrl == "" {
		t.Error("expected a UI descriptor with a bundle_url")
	}
	if len(d.Ui.Slots) != 1 || d.Ui.Slots[0].SlotId != "dashboard-widgets" {
		t.Errorf("expected one dashboard-widgets slot, got %v", d.Ui.Slots)
	}
	if len(d.Routes) != 1 || d.Routes[0].Path != "/events" {
		t.Errorf("expected a GET /events route, got %v", d.Routes)
	}
	if d.ConfigSchemaJson == "" {
		t.Error("expected a config_schema_json so the portal can render a settings form")
	}
}

func TestHandleEventRecordsEvent(t *testing.T) {
	h := newTestHandler()
	_, err := h.HandleEvent(context.Background(), connect.NewRequest(&pluginv1.HandleEventRequest{
		EventType: "run_completed",
		EventId:   "evt-1",
		Timestamp: "2026-05-22T10:00:00Z",
		Payload:   []byte(`{"run_id":"r1"}`),
	}))
	if err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}
	events := h.recentEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 recorded event, got %d", len(events))
	}
	if events[0].Type != "run_completed" || events[0].ID != "evt-1" {
		t.Errorf("recorded event mismatch: %+v", events[0])
	}
}

func TestRecordCapsAtMaxEvents(t *testing.T) {
	h := newTestHandler()
	for i := 0; i < defaultMaxEvents+10; i++ {
		h.record(event{Type: "run_completed", ID: "e"})
	}
	if got := len(h.recentEvents()); got != defaultMaxEvents {
		t.Fatalf("event buffer = %d, want capped at %d", got, defaultMaxEvents)
	}
}
