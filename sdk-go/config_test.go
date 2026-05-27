package sdk

import (
	"testing"
)

func TestLoadPluginEnv_Defaults(t *testing.T) {
	// Sanity: when nothing is set, defaults flow through.
	for _, k := range []string{"PLUGIN_NAME", "PLUGIN_ADDR", "GRPC_PORT", "RATD_URL", "RATD_INTERNAL_URL"} {
		t.Setenv(k, "")
	}
	env := LoadPluginEnv("myplugin", "50099", "myplugin:50099")
	if env.Name != "myplugin" {
		t.Errorf("Name = %q, want 'myplugin'", env.Name)
	}
	if env.Port != "50099" {
		t.Errorf("Port = %q, want '50099'", env.Port)
	}
	if env.Addr != "myplugin:50099" {
		t.Errorf("Addr = %q, want 'myplugin:50099'", env.Addr)
	}
	if env.RatdURL != "http://ratd:8080" {
		t.Errorf("RatdURL = %q, want default 'http://ratd:8080'", env.RatdURL)
	}
	if env.RatdInternalURL != "http://ratd:8080" {
		t.Errorf("RatdInternalURL should fall back to RatdURL, got %q", env.RatdInternalURL)
	}
}

func TestLoadPluginEnv_OverridesViaEnv(t *testing.T) {
	t.Setenv("PLUGIN_NAME", "custom-name")
	t.Setenv("PLUGIN_ADDR", "host.example:9999")
	t.Setenv("GRPC_PORT", "9999")
	t.Setenv("RATD_URL", "http://ratd.test:1234")
	t.Setenv("RATD_INTERNAL_URL", "http://ratd.internal:5678")

	env := LoadPluginEnv("default-name", "1111", "default:1111")
	if env.Name != "custom-name" {
		t.Errorf("Name = %q, want 'custom-name'", env.Name)
	}
	if env.Addr != "host.example:9999" {
		t.Errorf("Addr = %q, want 'host.example:9999'", env.Addr)
	}
	if env.Port != "9999" {
		t.Errorf("Port = %q, want '9999'", env.Port)
	}
	if env.RatdURL != "http://ratd.test:1234" {
		t.Errorf("RatdURL = %q, want override", env.RatdURL)
	}
	if env.RatdInternalURL != "http://ratd.internal:5678" {
		t.Errorf("RatdInternalURL = %q, want override", env.RatdInternalURL)
	}
}

func TestLoadPluginEnv_InternalFallsBackToPublic(t *testing.T) {
	t.Setenv("RATD_URL", "http://ratd.public:8080")
	t.Setenv("RATD_INTERNAL_URL", "")

	env := LoadPluginEnv("x", "1", "x:1")
	if env.RatdInternalURL != "http://ratd.public:8080" {
		t.Errorf("RatdInternalURL = %q, want fallback to RATD_URL", env.RatdInternalURL)
	}
}
