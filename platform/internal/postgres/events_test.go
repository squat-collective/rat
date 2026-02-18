package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryEventBus_PublishAndSubscribe(t *testing.T) {
	bus := postgres.NewMemoryEventBus()

	ch, cancel := bus.Subscribe(postgres.ChannelRunCompleted)
	defer cancel()

	payload := postgres.RunCompletedPayload{
		RunID:      "run-123",
		PipelineID: "pipe-456",
		Status:     "success",
	}

	err := bus.Publish(context.Background(), postgres.ChannelRunCompleted, payload)
	require.NoError(t, err)

	select {
	case event := <-ch:
		assert.Equal(t, postgres.ChannelRunCompleted, event.Channel)

		var got postgres.RunCompletedPayload
		require.NoError(t, json.Unmarshal(event.Payload, &got))
		assert.Equal(t, "run-123", got.RunID)
		assert.Equal(t, "pipe-456", got.PipelineID)
		assert.Equal(t, "success", got.Status)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMemoryEventBus_MultipleSubscribers(t *testing.T) {
	bus := postgres.NewMemoryEventBus()

	ch1, cancel1 := bus.Subscribe(postgres.ChannelRunCompleted)
	defer cancel1()
	ch2, cancel2 := bus.Subscribe(postgres.ChannelRunCompleted)
	defer cancel2()

	payload := postgres.RunCompletedPayload{
		RunID:  "run-1",
		Status: "success",
	}

	err := bus.Publish(context.Background(), postgres.ChannelRunCompleted, payload)
	require.NoError(t, err)

	// Both subscribers should receive the event.
	for i, ch := range []<-chan postgres.Event{ch1, ch2} {
		select {
		case event := <-ch:
			assert.Equal(t, postgres.ChannelRunCompleted, event.Channel, "subscriber %d", i)
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestMemoryEventBus_DifferentChannels(t *testing.T) {
	bus := postgres.NewMemoryEventBus()

	chRun, cancelRun := bus.Subscribe(postgres.ChannelRunCompleted)
	defer cancelRun()
	chPipe, cancelPipe := bus.Subscribe(postgres.ChannelPipelineCreated)
	defer cancelPipe()

	// Publish to run_completed only.
	err := bus.Publish(context.Background(), postgres.ChannelRunCompleted, postgres.RunCompletedPayload{
		RunID:  "run-1",
		Status: "success",
	})
	require.NoError(t, err)

	// Run channel should receive it.
	select {
	case event := <-chRun:
		assert.Equal(t, postgres.ChannelRunCompleted, event.Channel)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for run event")
	}

	// Pipeline channel should NOT receive it.
	select {
	case <-chPipe:
		t.Fatal("pipeline channel should not receive run_completed event")
	case <-time.After(50 * time.Millisecond):
		// Expected — no event on pipeline channel.
	}
}

func TestMemoryEventBus_CancelUnsubscribes(t *testing.T) {
	bus := postgres.NewMemoryEventBus()

	ch, cancel := bus.Subscribe(postgres.ChannelRunCompleted)

	// Cancel the subscription.
	cancel()

	// Publish after cancel — should not panic or block.
	err := bus.Publish(context.Background(), postgres.ChannelRunCompleted, postgres.RunCompletedPayload{
		RunID: "run-1",
	})
	require.NoError(t, err)

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed after cancel")
	case <-time.After(100 * time.Millisecond):
		// Also acceptable — event was dropped because subscriber was cancelled.
	}
}

func TestMemoryEventBus_Published_TracksAll(t *testing.T) {
	bus := postgres.NewMemoryEventBus()

	_ = bus.Publish(context.Background(), postgres.ChannelRunCompleted, postgres.RunCompletedPayload{RunID: "r1"})
	_ = bus.Publish(context.Background(), postgres.ChannelPipelineCreated, postgres.PipelineEventPayload{PipelineID: "p1"})

	published := bus.Published()
	require.Len(t, published, 2)
	assert.Equal(t, postgres.ChannelRunCompleted, published[0].Channel)
	assert.Equal(t, postgres.ChannelPipelineCreated, published[1].Channel)
}

func TestMemoryEventBus_PipelineEventPayload(t *testing.T) {
	bus := postgres.NewMemoryEventBus()

	ch, cancel := bus.Subscribe(postgres.ChannelPipelineUpdated)
	defer cancel()

	payload := postgres.PipelineEventPayload{
		PipelineID: "pipe-789",
		Namespace:  "default",
		Layer:      "silver",
		Name:       "orders",
	}

	err := bus.Publish(context.Background(), postgres.ChannelPipelineUpdated, payload)
	require.NoError(t, err)

	select {
	case event := <-ch:
		var got postgres.PipelineEventPayload
		require.NoError(t, json.Unmarshal(event.Payload, &got))
		assert.Equal(t, "pipe-789", got.PipelineID)
		assert.Equal(t, "default", got.Namespace)
		assert.Equal(t, "silver", got.Layer)
		assert.Equal(t, "orders", got.Name)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBus_ChannelConstants(t *testing.T) {
	// Verify channel names are stable — changing them would break existing subscribers.
	assert.Equal(t, "run_completed", postgres.ChannelRunCompleted)
	assert.Equal(t, "pipeline_created", postgres.ChannelPipelineCreated)
	assert.Equal(t, "pipeline_updated", postgres.ChannelPipelineUpdated)
}
