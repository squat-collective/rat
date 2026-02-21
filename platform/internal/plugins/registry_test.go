package plugins

import (
	"testing"

	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry_EmptyPlugins(t *testing.T) {
	reg := NewRegistry("community")
	assert.Empty(t, reg.All())
	assert.False(t, reg.AuthEnabled())
	assert.False(t, reg.ExecutorEnabled())
}

func TestRegistry_Register_SinglePlugin(t *testing.T) {
	reg := NewRegistry("pro")
	p := &Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}

	err := reg.Register(p)
	require.NoError(t, err)

	assert.True(t, reg.AuthEnabled())
	assert.NotNil(t, reg.Get("auth"))
	assert.Len(t, reg.All(), 1)
}

func TestRegistry_Register_MultipleCapabilities(t *testing.T) {
	reg := NewRegistry("pro")
	p := &Plugin{
		Name:         "acl-plugin",
		Addr:         "http://acl:50080",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapEnforcement, CapSharing},
	}

	err := reg.Register(p)
	require.NoError(t, err)

	assert.True(t, reg.EnforcementEnabled())
	assert.True(t, reg.SharingEnabled())
	assert.False(t, reg.AuthEnabled())
}

func TestRegistry_Register_CapabilityConflict(t *testing.T) {
	reg := NewRegistry("pro")

	p1 := &Plugin{
		Name:         "auth-keycloak",
		Addr:         "http://auth-keycloak:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}
	require.NoError(t, reg.Register(p1))

	p2 := &Plugin{
		Name:         "auth-okta",
		Addr:         "http://auth-okta:50061",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}
	err := reg.Register(p2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already held by")
}

func TestRegistry_Register_ReplacesSamePlugin(t *testing.T) {
	reg := NewRegistry("pro")

	p1 := &Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Version:      "1.0.0",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}
	require.NoError(t, reg.Register(p1))

	// Replacing the same plugin should succeed.
	p2 := &Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Version:      "2.0.0",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}
	require.NoError(t, reg.Register(p2))

	assert.True(t, reg.AuthEnabled())
	assert.Equal(t, "2.0.0", reg.Get("auth").Version)
	assert.Len(t, reg.All(), 1)
}

func TestRegistry_Remove(t *testing.T) {
	reg := NewRegistry("pro")
	p := &Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}
	require.NoError(t, reg.Register(p))
	assert.True(t, reg.AuthEnabled())

	reg.Remove("auth")

	assert.False(t, reg.AuthEnabled())
	assert.Nil(t, reg.Get("auth"))
	assert.Empty(t, reg.All())
}

func TestRegistry_Remove_NonExistent_NoOp(t *testing.T) {
	reg := NewRegistry("community")
	reg.Remove("nonexistent") // should not panic
}

func TestRegistry_ByCapability(t *testing.T) {
	reg := NewRegistry("pro")

	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}))
	require.NoError(t, reg.Register(&Plugin{
		Name:         "executor",
		Addr:         "http://executor:50070",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapExecutor},
	}))

	authPlugin := reg.ByCapability(CapAuth)
	require.NotNil(t, authPlugin)
	assert.Equal(t, "auth", authPlugin.Name)

	execPlugin := reg.ByCapability(CapExecutor)
	require.NotNil(t, execPlugin)
	assert.Equal(t, "executor", execPlugin.Name)

	assert.Nil(t, reg.ByCapability(CapCloud))
}

func TestRegistry_GetExecutorAddr(t *testing.T) {
	reg := NewRegistry("pro")
	assert.Equal(t, "", reg.GetExecutorAddr())

	require.NoError(t, reg.Register(&Plugin{
		Name:         "executor",
		Addr:         "http://executor:50070",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapExecutor},
	}))

	assert.Equal(t, "http://executor:50070", reg.GetExecutorAddr())
}

func TestRegistry_Features_CommunityDefaults(t *testing.T) {
	reg := NewRegistry("community")
	features := reg.Features()

	assert.Equal(t, "community", features.Edition)
	assert.False(t, features.Namespaces)
	assert.False(t, features.MultiUser)
	assert.False(t, features.Plugins["auth"].Enabled)
	assert.True(t, features.Plugins["executor"].Enabled)
	assert.Equal(t, "warmpool", features.Plugins["executor"].Type)
}

func TestRegistry_Features_ProWithAuth(t *testing.T) {
	reg := NewRegistry("pro")
	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}))

	features := reg.Features()

	assert.Equal(t, "pro", features.Edition)
	assert.True(t, features.Namespaces)
	assert.True(t, features.MultiUser)
	assert.True(t, features.Plugins["auth"].Enabled)
}

func TestRegistry_Features_ExecutorPlugin_ReportsContainer(t *testing.T) {
	reg := NewRegistry("pro")
	require.NoError(t, reg.Register(&Plugin{
		Name:         "executor",
		Addr:         "http://executor:50070",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapExecutor},
	}))

	features := reg.Features()

	assert.Equal(t, "container", features.Plugins["executor"].Type)
	assert.True(t, features.Plugins["executor"].Enabled)
}

func TestRegistry_AuthEnabled_RequiresEnabledStatus(t *testing.T) {
	reg := NewRegistry("pro")
	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusError,
		Capabilities: []string{CapAuth},
	}))

	assert.False(t, reg.AuthEnabled(), "error-status plugin should not count as enabled")
}

func TestRegistry_Remove_ClearsCapabilityIndex(t *testing.T) {
	reg := NewRegistry("pro")
	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}))

	reg.Remove("auth")

	// After removal, a different plugin should be able to claim the auth capability.
	err := reg.Register(&Plugin{
		Name:         "auth-v2",
		Addr:         "http://auth-v2:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	})
	assert.NoError(t, err)
	assert.Equal(t, "auth-v2", reg.Get("auth-v2").Name)
}

func TestPlugin_HasCapability(t *testing.T) {
	p := &Plugin{
		Capabilities: []string{CapAuth, CapEnforcement},
	}

	assert.True(t, p.HasCapability(CapAuth))
	assert.True(t, p.HasCapability(CapEnforcement))
	assert.False(t, p.HasCapability(CapCloud))
}

func TestEnsureScheme_AddsHTTP(t *testing.T) {
	assert.Equal(t, "http://auth:50060", EnsureScheme("auth:50060"))
}

func TestEnsureScheme_PreservesExisting(t *testing.T) {
	assert.Equal(t, "https://auth:50060", EnsureScheme("https://auth:50060"))
	assert.Equal(t, "http://auth:50060", EnsureScheme("http://auth:50060"))
}

func TestEnsureScheme_EmptyString(t *testing.T) {
	assert.Equal(t, "", EnsureScheme(""))
}

func TestInferCapabilitiesFromName(t *testing.T) {
	assert.Equal(t, []string{CapAuth}, InferCapabilitiesFromName("auth"))
	assert.Equal(t, []string{CapExecutor}, InferCapabilitiesFromName("executor"))
	assert.Equal(t, []string{CapSharing}, InferCapabilitiesFromName("sharing"))
	assert.Equal(t, []string{CapEnforcement}, InferCapabilitiesFromName("enforcement"))
	assert.Equal(t, []string{CapCloud}, InferCapabilitiesFromName("cloud"))
	assert.Nil(t, InferCapabilitiesFromName("unknown-plugin"))
}
