package plugins

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	connect "connectrpc.com/connect"
	"github.com/google/uuid"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

const eventDispatchTimeout = 5 * time.Second

// Well-known event channel names. Mirrors the constants in the postgres package
// to avoid an import cycle (plugins → postgres → api → plugins).
const (
	ChannelRunCompleted    = "run_completed"
	ChannelPipelineCreated = "pipeline_created"
	ChannelPipelineUpdated = "pipeline_updated"
)

// DispatchEvent represents a notification from the event bus.
type DispatchEvent struct {
	Channel string          `json:"channel"`
	Payload json.RawMessage `json:"payload"`
}

// DispatchEventBus abstracts the event bus for the dispatcher.
// Implemented by postgres.PgEventBus and postgres.MemoryEventBus.
type DispatchEventBus interface {
	Subscribe(channel string) (<-chan DispatchEvent, func())
}

// EventDispatcher subscribes to well-known event channels and fans out events
// to plugins whose EventTypes include the channel name.
type EventDispatcher struct {
	registry *Registry
	eventBus DispatchEventBus
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewEventDispatcher creates an event dispatcher that fans out events to plugins.
func NewEventDispatcher(registry *Registry, eventBus DispatchEventBus) *EventDispatcher {
	return &EventDispatcher{
		registry: registry,
		eventBus: eventBus,
	}
}

// Start subscribes to all well-known event channels and begins dispatching.
func (d *EventDispatcher) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	d.done = make(chan struct{})

	// Subscribe to well-known channels.
	channels := []string{
		ChannelRunCompleted,
		ChannelPipelineCreated,
		ChannelPipelineUpdated,
	}

	go func() {
		defer close(d.done)

		// Create subscriptions for each channel.
		type sub struct {
			channel string
			ch      <-chan DispatchEvent
			cancel  func()
		}
		subs := make([]sub, 0, len(channels))
		for _, channel := range channels {
			ch, cancelSub := d.eventBus.Subscribe(channel)
			subs = append(subs, sub{channel: channel, ch: ch, cancel: cancelSub})
		}

		// Cleanup subscriptions on exit.
		defer func() {
			for _, s := range subs {
				s.cancel()
			}
		}()

		// Multiplex all channels into a single select.
		// Since Go doesn't support dynamic select, we use a goroutine per channel.
		merged := make(chan DispatchEvent, 32)
		for _, s := range subs {
			s := s
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case event, ok := <-s.ch:
						if !ok {
							return
						}
						select {
						case merged <- event:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case event := <-merged:
				d.dispatch(ctx, event)
			}
		}
	}()

	slog.Info("event dispatcher started", "channels", channels)
}

// Stop cancels the dispatcher and waits for it to finish.
func (d *EventDispatcher) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.done != nil {
		<-d.done
	}
	slog.Info("event dispatcher stopped")
}

// dispatch fans out an event to all plugins that subscribe to its channel.
func (d *EventDispatcher) dispatch(ctx context.Context, event DispatchEvent) {
	plugins := d.registry.All()

	for _, p := range plugins {
		if !p.subscribedTo(event.Channel) {
			continue
		}
		if p.PluginClient == nil {
			continue
		}

		// Dispatch async with timeout — best-effort, don't block other plugins.
		go func(p *Plugin) {
			dispatchCtx, cancel := context.WithTimeout(ctx, eventDispatchTimeout)
			defer cancel()

			eventID := uuid.New().String()
			_, err := p.PluginClient.HandleEvent(dispatchCtx, connect.NewRequest(&pluginv1.HandleEventRequest{
				EventType: event.Channel,
				Payload:   event.Payload,
				EventId:   eventID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}))
			if err != nil {
				slog.Warn("event dispatch failed",
					"plugin", p.Name, "event", event.Channel, "error", err)
			}
		}(p)
	}
}

// subscribedTo returns true if the plugin's EventTypes includes the channel.
func (p *Plugin) subscribedTo(channel string) bool {
	for _, et := range p.EventTypes {
		if et == channel {
			return true
		}
	}
	return false
}
