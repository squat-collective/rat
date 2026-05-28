package plugins

import (
	"context"
	"errors"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthLoop_StartStop(t *testing.T) {
	reg := NewRegistry("community")
	hl := NewHealthLoop(reg, nil)
	hl.interval = 10 * time.Millisecond

	hl.Start(context.Background())
	time.Sleep(20 * time.Millisecond)
	hl.Stop()
}

func TestHealthLoop_TransitionsEnabledToError(t *testing.T) {
	reg := NewRegistry("pro")
	catalog := newMemoryCatalog()

	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	}))

	hl := NewHealthLoop(reg, catalog)
	hl.checkAll(context.Background())

	p := reg.Get("auth")
	assert.Equal(t, domain.PluginStatusError, p.Status)
	assert.NotEmpty(t, p.Error)
}

func TestHealthLoop_TransitionsErrorToEnabled(t *testing.T) {
	reg := NewRegistry("pro")
	catalog := newMemoryCatalog()

	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return connect.NewResponse(&pluginv1.HealthCheckResponse{
				Status: pluginv1.Status_STATUS_SERVING,
			}), nil
		},
	}

	// Start with error status.
	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusError,
		Error:        "was down",
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	}))

	hl := NewHealthLoop(reg, catalog)
	hl.checkAll(context.Background())

	p := reg.Get("auth")
	assert.Equal(t, domain.PluginStatusEnabled, p.Status)
	assert.Empty(t, p.Error)
}

func TestHealthLoop_SkipsDisabledPlugins(t *testing.T) {
	reg := NewRegistry("pro")

	callCount := 0
	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			callCount++
			return connect.NewResponse(&pluginv1.HealthCheckResponse{
				Status: pluginv1.Status_STATUS_SERVING,
			}), nil
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusDisabled,
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	}))

	hl := NewHealthLoop(reg, nil)
	hl.checkAll(context.Background())

	assert.Equal(t, 0, callCount, "disabled plugin should not be health-checked")
}

func TestHealthLoop_SkipsNilClient(t *testing.T) {
	reg := NewRegistry("pro")
	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
		PluginClient: nil,
	}))

	hl := NewHealthLoop(reg, nil)
	hl.checkAll(context.Background()) // should not panic
}

func TestHealthLoop_OnTransition_FiredOnEnabledToError(t *testing.T) {
	reg := NewRegistry("pro")

	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "executor",
		Addr:         "http://executor:50070",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapExecutor},
		PluginClient: mock,
	}))

	var transitionPlugin *Plugin
	var oldStatus, newStatus domain.PluginStatus

	hl := NewHealthLoop(reg, nil)
	hl.OnTransition = func(p *Plugin, oldSt, newSt domain.PluginStatus) {
		transitionPlugin = p
		oldStatus = oldSt
		newStatus = newSt
	}
	hl.checkAll(context.Background())

	require.NotNil(t, transitionPlugin, "OnTransition should have been called")
	assert.Equal(t, "executor", transitionPlugin.Name)
	assert.Equal(t, domain.PluginStatusEnabled, oldStatus)
	assert.Equal(t, domain.PluginStatusError, newStatus)
}

func TestHealthLoop_OnTransition_FiredOnErrorToEnabled(t *testing.T) {
	reg := NewRegistry("pro")

	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return connect.NewResponse(&pluginv1.HealthCheckResponse{
				Status: pluginv1.Status_STATUS_SERVING,
			}), nil
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "executor",
		Addr:         "http://executor:50070",
		Status:       domain.PluginStatusError,
		Error:        "was down",
		Capabilities: []string{CapExecutor},
		PluginClient: mock,
	}))

	var transitionPlugin *Plugin
	var oldStatus, newStatus domain.PluginStatus

	hl := NewHealthLoop(reg, nil)
	hl.OnTransition = func(p *Plugin, oldSt, newSt domain.PluginStatus) {
		transitionPlugin = p
		oldStatus = oldSt
		newStatus = newSt
	}
	hl.checkAll(context.Background())

	require.NotNil(t, transitionPlugin, "OnTransition should have been called")
	assert.Equal(t, "executor", transitionPlugin.Name)
	assert.Equal(t, domain.PluginStatusError, oldStatus)
	assert.Equal(t, domain.PluginStatusEnabled, newStatus)
}

func TestHealthLoop_OnTransition_NotFiredWhenStable(t *testing.T) {
	reg := NewRegistry("pro")

	// Plugin is healthy and stays healthy — no transition.
	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return connect.NewResponse(&pluginv1.HealthCheckResponse{
				Status: pluginv1.Status_STATUS_SERVING,
			}), nil
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "executor",
		Addr:         "http://executor:50070",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapExecutor},
		PluginClient: mock,
	}))

	callCount := 0
	hl := NewHealthLoop(reg, nil)
	hl.OnTransition = func(_ *Plugin, _, _ domain.PluginStatus) {
		callCount++
	}
	hl.checkAll(context.Background())

	assert.Equal(t, 0, callCount, "OnTransition should not fire when health status is stable")
}

// TestHealthLoop_ReDescribesOnRecovery is the regression for the plugin-restart
// stale-token blind window. When a plugin recovers (error→enabled), the loop
// must re-Describe and refresh the platform_token so subsequent proxied calls
// inject the plugin's CURRENT token, not the one captured at first registration.
func TestHealthLoop_ReDescribesOnRecovery(t *testing.T) {
	reg := NewRegistry("pro")

	describeCalls := 0
	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return connect.NewResponse(&pluginv1.HealthCheckResponse{
				Status: pluginv1.Status_STATUS_SERVING,
			}), nil
		},
		describeFunc: func(_ context.Context, _ *connect.Request[pluginv1.DescribeRequest]) (*connect.Response[pluginv1.DescribeResponse], error) {
			describeCalls++
			return connect.NewResponse(&pluginv1.DescribeResponse{
				PlatformToken: "fresh-token-after-restart",
				Version:       "1.2.3",
				Capabilities:  []string{CapAuth},
			}), nil
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusError,
		Error:        "was down",
		Token:        "stale-token-from-before-restart",
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	}))

	hl := NewHealthLoop(reg, nil)
	hl.checkAll(context.Background())

	p := reg.Get("auth")
	assert.Equal(t, domain.PluginStatusEnabled, p.Status)
	assert.Equal(t, 1, describeCalls, "Describe should be called on recovery")
	assert.Equal(t, "fresh-token-after-restart", p.Token, "stale token must be replaced")
	assert.Equal(t, "1.2.3", p.Version)
}

// TestHealthLoop_RecoveryWithUnimplementedDescribe ensures a legacy plugin
// that doesn't implement Describe still recovers cleanly (token just stays
// at whatever it was — no panic, no error).
func TestHealthLoop_RecoveryWithUnimplementedDescribe(t *testing.T) {
	reg := NewRegistry("pro")

	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return connect.NewResponse(&pluginv1.HealthCheckResponse{
				Status: pluginv1.Status_STATUS_SERVING,
			}), nil
		},
		// Default Describe returns Unimplemented — legacy plugin path.
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "legacy",
		Addr:         "http://legacy:50060",
		Status:       domain.PluginStatusError,
		Error:        "was down",
		Token:        "legacy-token",
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	}))

	hl := NewHealthLoop(reg, nil)
	hl.checkAll(context.Background())

	p := reg.Get("legacy")
	assert.Equal(t, domain.PluginStatusEnabled, p.Status)
	assert.Equal(t, "legacy-token", p.Token, "legacy token preserved when Describe is unimplemented")
}

func TestHealthLoop_PersistsTransitionToCatalog(t *testing.T) {
	reg := NewRegistry("pro")
	catalog := newMemoryCatalog()
	catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:    "auth",
		Status:  domain.PluginStatusEnabled,
		Healthy: true,
	})

	mock := &mockPluginServiceClient{
		healthCheckFunc: func(_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
			return nil, errors.New("down")
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
		PluginClient: mock,
	}))

	hl := NewHealthLoop(reg, catalog)
	hl.checkAll(context.Background())

	entry, _ := catalog.GetPlugin(context.Background(), "auth")
	require.NotNil(t, entry)
	assert.False(t, entry.Healthy)
	assert.Equal(t, domain.PluginStatusError, entry.Status)
}
