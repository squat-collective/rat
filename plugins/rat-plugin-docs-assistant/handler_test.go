package main

import (
	"context"
	"testing"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

func TestHealthCheckServing(t *testing.T) {
	h := newHandler("docs-assistant", "http://docs-assistant:50096/bundle.js", "sha256-test", "test-token")
	resp, err := h.HealthCheck(context.Background(), connect.NewRequest(&pluginv1.HealthCheckRequest{}))
	if err != nil {
		t.Fatalf("HealthCheck error: %v", err)
	}
	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		t.Fatalf("expected STATUS_SERVING, got %v", resp.Msg.Status)
	}
}

func TestDescribeAdvertisesTableActionsSlot(t *testing.T) {
	h := newHandler("docs-assistant", "http://docs-assistant:50096/bundle.js", "sha256-test", "test-token")
	resp, err := h.Describe(context.Background(), connect.NewRequest(&pluginv1.DescribeRequest{}))
	if err != nil {
		t.Fatalf("Describe error: %v", err)
	}
	d := resp.Msg
	if d.Name != "docs-assistant" {
		t.Errorf("name = %q, want docs-assistant", d.Name)
	}
	if d.Ui == nil || len(d.Ui.Slots) != 1 {
		t.Fatalf("expected exactly one UI slot, got %v", d.Ui)
	}
	if d.Ui.Slots[0].SlotId != "table-actions" {
		t.Errorf("slot = %q, want table-actions", d.Ui.Slots[0].SlotId)
	}
	if d.Ui.Slots[0].ComponentName != "DocsAssistantButton" {
		t.Errorf("component = %q, want DocsAssistantButton", d.Ui.Slots[0].ComponentName)
	}
}
