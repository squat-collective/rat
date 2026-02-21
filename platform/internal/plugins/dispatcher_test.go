package plugins

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryDispatchBus is an in-memory DispatchEventBus for tests.
type memoryDispatchBus struct {
	mu          sync.Mutex
	subscribers map[string][]chan DispatchEvent
}

func newMemoryDispatchBus() *memoryDispatchBus {
	return &memoryDispatchBus{
		subscribers: make(map[string][]chan DispatchEvent),
	}
}

func (b *memoryDispatchBus) Subscribe(channel string) (<-chan DispatchEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan DispatchEvent, 16)
	b.subscribers[channel] = append(b.subscribers[channel], ch)

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[channel]
		for i, s := range subs {
			if s == ch {
				b.subscribers[channel] = append(subs[:i], subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
}

func (b *memoryDispatchBus) Publish(channel string, payload interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	data, _ := json.Marshal(payload)
	event := DispatchEvent{Channel: channel, Payload: data}
	for _, ch := range b.subscribers[channel] {
		select {
		case ch <- event:
		default:
		}
	}
}

func TestEventDispatcher_StartStop(t *testing.T) {
	bus := newMemoryDispatchBus()
	reg := NewRegistry("community")
	d := NewEventDispatcher(reg, bus)

	d.Start(context.Background())
	// Give the goroutines a moment to start.
	time.Sleep(10 * time.Millisecond)
	d.Stop()
}

func TestEventDispatcher_DispatchesToSubscribedPlugin(t *testing.T) {
	bus := newMemoryDispatchBus()
	reg := NewRegistry("pro")

	received := make(chan string, 1)
	mock := &mockPluginServiceClient{
		handleEventFunc: func(_ context.Context, req *connect.Request[pluginv1.HandleEventRequest]) (*connect.Response[pluginv1.HandleEventResponse], error) {
			received <- req.Msg.EventType
			return connect.NewResponse(&pluginv1.HandleEventResponse{}), nil
		},
	}

	require.NoError(t, reg.Register(&Plugin{
		Name:         "my-plugin",
		Addr:         "http://my-plugin:50090",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
		EventTypes:   []string{ChannelRunCompleted},
		PluginClient: mock,
	}))

	d := NewEventDispatcher(reg, bus)
	d.Start(context.Background())
	defer d.Stop()

	// Give the dispatcher goroutines a moment to start and subscribe.
	time.Sleep(50 * time.Millisecond)

	// Publish an event.
	bus.Publish(ChannelRunCompleted, map[string]string{
		"run_id": "run-123",
		"status": "success",
	})

	select {
	case eventType := <-received:
		assert.Equal(t, ChannelRunCompleted, eventType)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event dispatch")
	}
}

func TestEventDispatcher_IgnoresUnsubscribedPlugin(t *testing.T) {
	bus := newMemoryDispatchBus()
	reg := NewRegistry("pro")

	called := make(chan struct{}, 1)
	mock := &mockPluginServiceClient{
		handleEventFunc: func(_ context.Context, _ *connect.Request[pluginv1.HandleEventRequest]) (*connect.Response[pluginv1.HandleEventResponse], error) {
			called <- struct{}{}
			return connect.NewResponse(&pluginv1.HandleEventResponse{}), nil
		},
	}

	// Plugin subscribes to pipeline_created, NOT run_completed.
	require.NoError(t, reg.Register(&Plugin{
		Name:         "pipeline-watcher",
		Addr:         "http://watcher:50090",
		Status:       domain.PluginStatusEnabled,
		EventTypes:   []string{ChannelPipelineCreated},
		PluginClient: mock,
	}))

	d := NewEventDispatcher(reg, bus)
	d.Start(context.Background())
	defer d.Stop()

	// Publish a run_completed event.
	bus.Publish(ChannelRunCompleted, map[string]string{"run_id": "run-1"})

	// Plugin should NOT receive it.
	select {
	case <-called:
		t.Fatal("plugin should not have received the event")
	case <-time.After(200 * time.Millisecond):
		// Expected — no call.
	}
}

func TestPlugin_SubscribedTo(t *testing.T) {
	p := &Plugin{EventTypes: []string{"run_completed", "pipeline_created"}}
	assert.True(t, p.subscribedTo("run_completed"))
	assert.True(t, p.subscribedTo("pipeline_created"))
	assert.False(t, p.subscribedTo("pipeline_updated"))
}
