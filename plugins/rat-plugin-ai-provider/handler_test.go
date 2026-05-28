package main

import (
	"context"
	"encoding/json"
	"testing"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

func TestHealthCheckServing(t *testing.T) {
	h := newHandler("ai-provider", "http://ai-provider:50094/bundle.js", "sha256-test", "test-token")
	resp, err := h.HealthCheck(context.Background(), connect.NewRequest(&pluginv1.HealthCheckRequest{}))
	if err != nil {
		t.Fatalf("HealthCheck error: %v", err)
	}
	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		t.Fatalf("expected STATUS_SERVING, got %v", resp.Msg.Status)
	}
}

func TestDescribeCarriesConfigSchema(t *testing.T) {
	h := newHandler("ai-provider", "http://ai-provider:50094/bundle.js", "sha256-test", "test-token")
	resp, err := h.Describe(context.Background(), connect.NewRequest(&pluginv1.DescribeRequest{}))
	if err != nil {
		t.Fatalf("Describe error: %v", err)
	}
	d := resp.Msg
	if d.Name != "ai-provider" {
		t.Errorf("name = %q, want ai-provider", d.Name)
	}
	// The whole point of this plugin: it advertises a config schema.
	if d.ConfigSchemaJson == "" {
		t.Fatal("expected a config_schema_json")
	}
	if !json.Valid([]byte(d.ConfigSchemaJson)) {
		t.Fatal("config_schema_json must be valid JSON")
	}
	if len(d.Routes) == 0 {
		t.Error("expected the provider API routes to be advertised")
	}
	if d.Ui == nil || len(d.Ui.NavItems) != 1 || d.Ui.NavItems[0].Path != "/x/ai-provider" {
		t.Errorf("expected an /x/ai-provider nav item, got %v", d.Ui)
	}
	if len(d.Ui.Routes) != 1 || d.Ui.Routes[0].ComponentName != "AIProviderApp" {
		t.Errorf("expected an AIProviderApp UI route, got %v", d.Ui)
	}
}
