package main

import (
	"context"
	"testing"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

func TestHealthCheckServing(t *testing.T) {
	h := newHandler("interconnect", "http://interconnect:50093/bundle.js", "sha256-test", "test-token")
	resp, err := h.HealthCheck(context.Background(), connect.NewRequest(&pluginv1.HealthCheckRequest{}))
	if err != nil {
		t.Fatalf("HealthCheck error: %v", err)
	}
	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		t.Fatalf("expected STATUS_SERVING, got %v", resp.Msg.Status)
	}
}

func TestDescribeAdvertisesMeshUI(t *testing.T) {
	h := newHandler("interconnect", "http://interconnect:50093/bundle.js", "sha256-test", "test-token")
	resp, err := h.Describe(context.Background(), connect.NewRequest(&pluginv1.DescribeRequest{}))
	if err != nil {
		t.Fatalf("Describe error: %v", err)
	}
	d := resp.Msg
	if d.Name != "interconnect" {
		t.Errorf("name = %q, want interconnect", d.Name)
	}
	if len(d.Routes) == 0 {
		t.Error("expected the interconnect API routes to be advertised")
	}
	if d.Ui == nil || len(d.Ui.NavItems) != 1 || d.Ui.NavItems[0].Path != "/x/interconnect" {
		t.Errorf("expected an /x/interconnect nav item, got %v", d.Ui)
	}
	if len(d.Ui.Routes) != 1 || d.Ui.Routes[0].ComponentName != "InterconnectApp" {
		t.Errorf("expected an InterconnectApp UI route, got %v", d.Ui)
	}
}
