package sdk

import (
	"testing"

	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

func TestDescribeBuilder_MinimumFields(t *testing.T) {
	resp := NewDescribe("myplugin", "1.2.3", "does things").Build()
	if resp.Name != "myplugin" {
		t.Errorf("Name = %q, want 'myplugin'", resp.Name)
	}
	if resp.Version != "1.2.3" {
		t.Errorf("Version = %q, want '1.2.3'", resp.Version)
	}
	if resp.Description != "does things" {
		t.Errorf("Description = %q, want 'does things'", resp.Description)
	}
	if resp.Ui != nil {
		t.Error("Ui should be nil when WithUI not called")
	}
	if resp.PlatformToken != "" {
		t.Error("PlatformToken should be empty when not set")
	}
	if len(resp.Routes) != 0 {
		t.Error("Routes should be empty when no WithRoute calls")
	}
}

func TestDescribeBuilder_AccumulatesRoutes(t *testing.T) {
	resp := NewDescribe("p", "0.1.0", "x").
		WithRoute("GET", "/a", "list a").
		WithRoute("POST", "/a", "create a").
		WithRoute("DELETE", "/a/{id}", "drop a").
		Build()
	if len(resp.Routes) != 3 {
		t.Fatalf("Routes len = %d, want 3", len(resp.Routes))
	}
	if resp.Routes[0].Method != "GET" || resp.Routes[0].Path != "/a" {
		t.Errorf("route 0 = %+v", resp.Routes[0])
	}
	if resp.Routes[2].Method != "DELETE" || resp.Routes[2].Path != "/a/{id}" {
		t.Errorf("route 2 = %+v", resp.Routes[2])
	}
}

func TestDescribeBuilder_FullChain(t *testing.T) {
	nav := []*pluginv1.UINavItem{{Label: "Demo", Icon: "rocket", Path: "/x/demo", Priority: 5}}
	routes := []*pluginv1.UIRoute{{Path: "/x/demo", ComponentName: "DemoApp"}}
	resp := NewDescribe("demo", "0.1.0", "demo plugin").
		WithRoute("GET", "/things", "list things").
		WithUI("http://demo:50100/bundle.js", "sha256-abc", nav, routes).
		WithPlatformToken("hex-token-xyz").
		WithConfigSchema(`{"type":"object"}`).
		WithCapabilities("sharing", "executor").
		WithEventSubscriptions("run_completed").
		WithProvidesWorker(true).
		Build()

	if resp.Ui == nil {
		t.Fatal("Ui should be set")
	}
	if resp.Ui.BundleUrl != "http://demo:50100/bundle.js" {
		t.Errorf("BundleUrl = %q", resp.Ui.BundleUrl)
	}
	if resp.Ui.BundleHash != "sha256-abc" {
		t.Errorf("BundleHash = %q", resp.Ui.BundleHash)
	}
	if len(resp.Ui.NavItems) != 1 || resp.Ui.NavItems[0].Label != "Demo" {
		t.Errorf("NavItems = %+v", resp.Ui.NavItems)
	}
	if resp.PlatformToken != "hex-token-xyz" {
		t.Errorf("PlatformToken = %q", resp.PlatformToken)
	}
	if resp.ConfigSchemaJson != `{"type":"object"}` {
		t.Errorf("ConfigSchemaJson = %q", resp.ConfigSchemaJson)
	}
	if len(resp.Capabilities) != 2 || resp.Capabilities[0] != "sharing" {
		t.Errorf("Capabilities = %v", resp.Capabilities)
	}
	if len(resp.EventSubscriptions) != 1 || resp.EventSubscriptions[0] != "run_completed" {
		t.Errorf("EventSubscriptions = %v", resp.EventSubscriptions)
	}
	if !resp.ProvidesWorker {
		t.Error("ProvidesWorker should be true")
	}
}
