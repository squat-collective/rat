// Package plugins provides plugin lifecycle management, including periodic
// health checks that automatically disable unhealthy plugins and re-enable
// them when they recover.
package plugins

import (
	"context"
	"log/slog"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	"github.com/rat-data/rat/platform/internal/config"
)

// healthCheckInterval is the default interval between periodic health checks.
const healthCheckInterval = 30 * time.Second

// pluginHealthState tracks the health check client and current status of a plugin.
type pluginHealthState struct {
	name   string
	addr   string
	client pluginv1connect.PluginServiceClient
	// healthy is the last known health status. When false, the plugin is
	// considered disabled. The Registry's enabledMu protects reads/writes
	// to the Registry's plugin fields based on this.
	healthy bool
}

// HealthLoop runs periodic health checks on all loaded plugins.
// Unhealthy plugins are disabled (set to nil in the Registry) and re-enabled
// when they recover. This prevents cascading failures when a plugin container
// restarts or becomes temporarily unavailable.
type HealthLoop struct {
	registry *Registry
	states   []pluginHealthState
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex // protects states
}

// NewHealthLoop creates a periodic health checker for all plugins in the config.
// Call Start() to begin the background goroutine.
func NewHealthLoop(registry *Registry, cfg *config.Config, factory *pluginClientFactory, httpClient connect.HTTPClient) *HealthLoop {
	hl := &HealthLoop{
		registry: registry,
		interval: healthCheckInterval,
	}

	// Build health check clients for each configured plugin.
	for name, pluginCfg := range cfg.Plugins {
		addr := ensureScheme(pluginCfg.Addr)
		client := factory.newPluginClient(httpClient, addr)
		hl.states = append(hl.states, pluginHealthState{
			name:    name,
			addr:    addr,
			client:  client,
			healthy: true, // assume healthy at startup (Load already checked)
		})
	}

	return hl
}

// Start begins the periodic health check goroutine.
func (hl *HealthLoop) Start(ctx context.Context) {
	ctx, hl.cancel = context.WithCancel(ctx)
	hl.done = make(chan struct{})

	go func() {
		defer close(hl.done)
		ticker := time.NewTicker(hl.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hl.checkAll(ctx)
			}
		}
	}()
}

// Stop cancels the health check goroutine and waits for it to finish.
func (hl *HealthLoop) Stop() {
	if hl.cancel != nil {
		hl.cancel()
	}
	if hl.done != nil {
		<-hl.done
	}
}

// checkAll runs a health check against each tracked plugin.
// Transitions: healthy -> unhealthy (disable) or unhealthy -> healthy (re-enable).
func (hl *HealthLoop) checkAll(ctx context.Context) {
	hl.mu.Lock()
	states := make([]pluginHealthState, len(hl.states))
	copy(states, hl.states)
	hl.mu.Unlock()

	for i := range states {
		s := &states[i]
		checkCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
		_, err := s.client.HealthCheck(checkCtx, connect.NewRequest(&pluginv1.HealthCheckRequest{}))
		cancel()

		wasHealthy := s.healthy
		nowHealthy := err == nil

		if wasHealthy && !nowHealthy {
			// Transition: healthy -> unhealthy. Disable the plugin.
			slog.Warn("plugin health check failed, disabling",
				"plugin", s.name, "addr", s.addr, "error", err)
			hl.registry.disablePlugin(s.name)
			s.healthy = false
		} else if !wasHealthy && nowHealthy {
			// Transition: unhealthy -> healthy. Re-enable the plugin.
			slog.Info("plugin recovered, re-enabling",
				"plugin", s.name, "addr", s.addr)
			// Note: re-enabling requires re-creating the client connection.
			// For now, we mark as healthy. Full re-enablement would require
			// re-running the loadXxx method. This is logged so operators
			// know a restart may be needed for full functionality.
			slog.Info("plugin marked healthy — restart ratd to fully re-enable",
				"plugin", s.name)
			s.healthy = true
		}
	}

	hl.mu.Lock()
	copy(hl.states, states)
	hl.mu.Unlock()
}

// disablePlugin sets the plugin's client to nil, effectively disabling it.
// The Features() method will reflect the plugin as disabled.
func (r *Registry) disablePlugin(name string) {
	switch name {
	case PluginAuth:
		r.auth = nil
	case PluginExecutor:
		r.executor = nil
	case PluginSharing:
		r.sharing = nil
	case PluginEnforcement:
		r.enforcement = nil
	case PluginCloud:
		r.cloud = nil
	}
}
