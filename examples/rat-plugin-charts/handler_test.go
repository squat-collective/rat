package main

import (
	"context"
	"testing"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

func TestHealthCheckServing(t *testing.T) {
	h := newHandler("charts", "http://charts:50092/bundle.js")
	resp, err := h.HealthCheck(context.Background(), connect.NewRequest(&pluginv1.HealthCheckRequest{}))
	if err != nil {
		t.Fatalf("HealthCheck error: %v", err)
	}
	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		t.Fatalf("expected STATUS_SERVING, got %v", resp.Msg.Status)
	}
}

func TestDescribeAdvertisesRoutesAndUI(t *testing.T) {
	h := newHandler("charts", "http://charts:50092/bundle.js")
	resp, err := h.Describe(context.Background(), connect.NewRequest(&pluginv1.DescribeRequest{}))
	if err != nil {
		t.Fatalf("Describe error: %v", err)
	}
	d := resp.Msg
	if d.Name != "charts" {
		t.Errorf("name = %q, want charts", d.Name)
	}
	if len(d.Routes) == 0 {
		t.Error("expected the charts API routes to be advertised")
	}
	if d.Ui == nil || d.Ui.BundleUrl == "" {
		t.Fatal("expected a UI descriptor with a bundle_url")
	}
	if len(d.Ui.NavItems) != 1 || d.Ui.NavItems[0].Path != "/x/charts" {
		t.Errorf("expected an /x/charts nav item, got %v", d.Ui.NavItems)
	}
	if len(d.Ui.Routes) != 1 || d.Ui.Routes[0].ComponentName != "ChartsApp" {
		t.Errorf("expected a ChartsApp UI route, got %v", d.Ui.Routes)
	}
}
