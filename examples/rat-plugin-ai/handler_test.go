package main

import (
	"context"
	"strings"
	"testing"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

func TestHealthCheckServing(t *testing.T) {
	h := newHandler("ai", "http://ai:50091/bundle.js")
	resp, err := h.HealthCheck(context.Background(), connect.NewRequest(&pluginv1.HealthCheckRequest{}))
	if err != nil {
		t.Fatalf("HealthCheck error: %v", err)
	}
	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		t.Fatalf("expected STATUS_SERVING, got %v", resp.Msg.Status)
	}
}

func TestDescribeAdvertisesChatRouteAndUI(t *testing.T) {
	h := newHandler("ai", "http://ai:50091/bundle.js")
	resp, err := h.Describe(context.Background(), connect.NewRequest(&pluginv1.DescribeRequest{}))
	if err != nil {
		t.Fatalf("Describe error: %v", err)
	}
	d := resp.Msg
	if d.Name != "ai" {
		t.Errorf("name = %q, want ai", d.Name)
	}
	if len(d.Routes) != 1 || d.Routes[0].Path != "/chat" {
		t.Errorf("expected a /chat route, got %v", d.Routes)
	}
	if d.Ui == nil || d.Ui.BundleUrl == "" {
		t.Fatal("expected a UI descriptor with a bundle_url")
	}
	if len(d.Ui.NavItems) != 1 || d.Ui.NavItems[0].Path != "/x/ai" {
		t.Errorf("expected an /x/ai nav item, got %v", d.Ui.NavItems)
	}
}

func TestToolErrorIsJSON(t *testing.T) {
	got := toolError(context.DeadlineExceeded)
	if got == "" || got[0] != '{' {
		t.Fatalf("toolError should return a JSON object, got %q", got)
	}
}

func TestExecuteUnknownTool(t *testing.T) {
	tools := newDataTools("http://ratd:8080")
	out := tools.execute(context.Background(), "no_such_tool", "{}")
	if out == "" {
		t.Fatal("expected an error payload for an unknown tool")
	}
}

func TestCleanQueryResultStripsAmbiguousFields(t *testing.T) {
	// total_rows can be mistaken by small models for a value inside the rows.
	raw := `{"columns":[{"name":"c"}],"rows":[{"c":2}],"total_rows":1,"duration_ms":5}`
	out := cleanQueryResult(raw)
	if strings.Contains(out, "total_rows") || strings.Contains(out, "duration_ms") {
		t.Errorf("total_rows/duration_ms should be stripped, got %s", out)
	}
	if !strings.Contains(out, `"rows"`) || !strings.Contains(out, `"c":2`) {
		t.Errorf("columns and rows should be preserved, got %s", out)
	}
}

func TestCleanQueryResultPassesThroughErrors(t *testing.T) {
	raw := `{"error":"bad sql"}`
	if cleanQueryResult(raw) != raw {
		t.Error("an error payload should pass through unchanged")
	}
}
