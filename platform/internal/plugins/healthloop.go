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
	"github.com/rat-data/rat/platform/internal/domain"
)

// healthCheckInterval is the default interval between periodic health checks.
const healthCheckInterval = 30 * time.Second

// HealthLoop runs periodic health checks on all registered plugins.
// Unhealthy plugins transition to error status; recovered plugins are re-enabled.
// Uses the live Registry to iterate all plugins dynamically.
type HealthLoop struct {
	registry *Registry
	catalog  PluginCatalog // optional — for persisting health transitions
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex

	// OnTransition is called after a plugin's health status changes.
	// Fired on enabled→error and error→enabled transitions.
	// Set by the caller (main.go) to trigger re-wiring (e.g., executor fallback).
	OnTransition func(plugin *Plugin, oldStatus, newStatus domain.PluginStatus)
}

// NewHealthLoop creates a periodic health checker that iterates the registry.
// Pass nil catalog if no persistence is desired (tests).
func NewHealthLoop(registry *Registry, catalog PluginCatalog) *HealthLoop {
	return &HealthLoop{
		registry: registry,
		catalog:  catalog,
		interval: healthCheckInterval,
	}
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

// checkAll runs a health check against each registered plugin.
// Transitions: enabled→error (disable) or error→enabled (re-enable).
func (hl *HealthLoop) checkAll(ctx context.Context) {
	hl.mu.Lock()
	defer hl.mu.Unlock()

	plugins := hl.registry.All()

	for _, p := range plugins {
		// Skip disabled plugins — they need explicit Enable() to restart.
		if p.Status == domain.PluginStatusDisabled {
			continue
		}

		if p.PluginClient == nil {
			continue
		}

		checkCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
		_, err := p.PluginClient.HealthCheck(checkCtx, connect.NewRequest(&pluginv1.HealthCheckRequest{}))
		cancel()

		wasHealthy := p.Status == domain.PluginStatusEnabled
		nowHealthy := err == nil

		if wasHealthy && !nowHealthy {
			// Transition: enabled → error. Mark as error.
			slog.Warn("plugin health check failed, marking as error",
				"plugin", p.Name, "addr", p.Addr, "error", err)
			p.Status = domain.PluginStatusError
			p.Error = err.Error()

			if hl.catalog != nil {
				_ = hl.catalog.UpdatePluginHealth(ctx, p.Name, false, err.Error())
				_ = hl.catalog.UpdatePluginStatus(ctx, p.Name, domain.PluginStatusError, err.Error())
			}
			if hl.OnTransition != nil {
				hl.OnTransition(p, domain.PluginStatusEnabled, domain.PluginStatusError)
			}
		} else if !wasHealthy && nowHealthy {
			// Transition: error → enabled. Re-enable.
			slog.Info("plugin recovered, re-enabling",
				"plugin", p.Name, "addr", p.Addr)
			p.Status = domain.PluginStatusEnabled
			p.Error = ""

			if hl.catalog != nil {
				_ = hl.catalog.UpdatePluginHealth(ctx, p.Name, true, "")
				_ = hl.catalog.UpdatePluginStatus(ctx, p.Name, domain.PluginStatusEnabled, "")
			}
			if hl.OnTransition != nil {
				hl.OnTransition(p, domain.PluginStatusError, domain.PluginStatusEnabled)
			}
		}
	}
}
