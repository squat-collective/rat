// Package postgres — events.go implements a lightweight event bus using
// Postgres LISTEN/NOTIFY. This enables instant reaction to database changes
// (e.g. run completion, pipeline create/update) without polling.
//
// PgEventBus acquires a dedicated *pgx.Conn (not from the pool) to hold
// persistent LISTEN channels. The pool's regular connections remain free
// for queries. NOTIFY calls go through the pool — no dedicated connection needed.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Well-known channel names for LISTEN/NOTIFY.
const (
	ChannelRunCompleted      = "run_completed"
	ChannelPipelineCreated   = "pipeline_created"
	ChannelPipelineUpdated   = "pipeline_updated"
	ChannelPipelinePublished = "pipeline_published"
	ChannelPipelineDeleted   = "pipeline_deleted"
	ChannelFileUploaded      = "file_uploaded"
	ChannelQualityFailed     = "quality_failed"
	ChannelScheduleFired     = "schedule_fired"
)

// allChannels lists every channel PgEventBus listens on. They are LISTENed
// up-front in Start(): LISTEN cannot run concurrently with the listener loop's
// WaitForNotification on the same pgx connection (it would fail "conn busy").
var allChannels = []string{
	ChannelRunCompleted,
	ChannelPipelineCreated,
	ChannelPipelineUpdated,
	ChannelPipelinePublished,
	ChannelPipelineDeleted,
	ChannelFileUploaded,
	ChannelQualityFailed,
	ChannelScheduleFired,
}

// Event represents a single notification received from Postgres NOTIFY.
type Event struct {
	Channel string          `json:"channel"`
	Payload json.RawMessage `json:"payload"`
}

// RunCompletedPayload is the JSON payload for run_completed events.
type RunCompletedPayload struct {
	RunID      string `json:"run_id"`
	PipelineID string `json:"pipeline_id"`
	Status     string `json:"status"`
}

// PipelineEventPayload is the JSON payload for pipeline_created/updated/published/deleted events.
type PipelineEventPayload struct {
	PipelineID string `json:"pipeline_id"`
	Namespace  string `json:"namespace"`
	Layer      string `json:"layer"`
	Name       string `json:"name"`
}

// FileUploadedPayload is the JSON payload for file_uploaded events.
type FileUploadedPayload struct {
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
	Size      int64  `json:"size"`
}

// QualityFailedPayload is the JSON payload for quality_failed events.
type QualityFailedPayload struct {
	PipelineID string `json:"pipeline_id,omitempty"`
	Namespace  string `json:"namespace"`
	Layer      string `json:"layer"`
	Name       string `json:"name"`
	Failed     int    `json:"failed"`
	Total      int    `json:"total"`
}

// ScheduleFiredPayload is the JSON payload for schedule_fired events.
type ScheduleFiredPayload struct {
	ScheduleID string `json:"schedule_id"`
	PipelineID string `json:"pipeline_id"`
	RunID      string `json:"run_id"`
	CronExpr   string `json:"cron_expr"`
}

// EventBus defines the interface for publishing and subscribing to events.
// This allows non-Postgres implementations (e.g. in-memory for tests).
type EventBus interface {
	// Publish sends a NOTIFY on the given channel with a JSON payload.
	Publish(ctx context.Context, channel string, payload interface{}) error

	// Subscribe registers a listener for the given channel and returns
	// a read-only channel of events. The caller should call the returned
	// cancel function to unsubscribe and close the channel.
	Subscribe(channel string) (<-chan Event, func())
}

// PgEventBus implements EventBus using Postgres LISTEN/NOTIFY.
// It uses a dedicated pgx.Conn for LISTEN (long-lived) and the pool for NOTIFY.
type PgEventBus struct {
	pool       *pgxpool.Pool
	listenConn *pgx.Conn

	mu          sync.Mutex
	subscribers map[string][]subscriber // channel -> list of subscribers
	listening   map[string]bool         // channels we've already LISTENed on

	cancel context.CancelFunc
	done   chan struct{}
}

// subscriber holds a single subscriber's delivery channel and done signal.
type subscriber struct {
	ch   chan Event
	done chan struct{} // closed when unsubscribed
}

// NewPgEventBus creates a new event bus. Call Start() to begin listening.
func NewPgEventBus(pool *pgxpool.Pool) *PgEventBus {
	return &PgEventBus{
		pool:        pool,
		subscribers: make(map[string][]subscriber),
		listening:   make(map[string]bool),
	}
}

// Start acquires a dedicated connection and begins the notification listener loop.
// The loop runs until ctx is cancelled or Stop() is called.
func (eb *PgEventBus) Start(ctx context.Context) error {
	// Acquire a dedicated connection outside the pool for LISTEN.
	connConfig := eb.pool.Config().ConnConfig.Copy()
	conn, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		return fmt.Errorf("event bus: acquire listen connection: %w", err)
	}
	eb.listenConn = conn

	// LISTEN on every well-known channel now, while we have exclusive use of
	// the connection. Once listenLoop starts, WaitForNotification holds the
	// connection and a concurrent LISTEN from Subscribe fails with "conn busy".
	eb.mu.Lock()
	for _, ch := range allChannels {
		if _, err := conn.Exec(ctx, "LISTEN "+ch); err != nil {
			eb.mu.Unlock()
			_ = conn.Close(context.Background())
			return fmt.Errorf("event bus: LISTEN %s: %w", ch, err)
		}
		eb.listening[ch] = true
	}
	eb.mu.Unlock()

	ctx, eb.cancel = context.WithCancel(ctx)
	eb.done = make(chan struct{})

	go eb.listenLoop(ctx)

	slog.Info("event bus started", "channels", allChannels)
	return nil
}

// Stop cancels the listener loop and closes the dedicated connection.
func (eb *PgEventBus) Stop() {
	if eb.cancel != nil {
		eb.cancel()
	}
	if eb.done != nil {
		<-eb.done
	}
	if eb.listenConn != nil {
		// Use a fresh context since our main context is cancelled.
		_ = eb.listenConn.Close(context.Background())
	}
	slog.Info("event bus stopped")
}

// Publish sends a NOTIFY on the given channel. The payload is JSON-serialized.
// Uses the pool (not the dedicated listen connection).
func (eb *PgEventBus) Publish(ctx context.Context, channel string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("event bus: marshal payload: %w", err)
	}

	_, err = eb.pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, string(data))
	if err != nil {
		return fmt.Errorf("event bus: notify %s: %w", channel, err)
	}
	return nil
}

// Subscribe registers a listener for the given channel. Returns a read-only
// event channel and a cancel function. The event channel is buffered (16) to
// avoid blocking the listener loop on slow consumers.
//
// The first subscriber on a channel triggers a LISTEN command on the dedicated
// connection.
func (eb *PgEventBus) Subscribe(channel string) (_ <-chan Event, _ func()) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	sub := subscriber{
		ch:   make(chan Event, 16),
		done: make(chan struct{}),
	}
	eb.subscribers[channel] = append(eb.subscribers[channel], sub)

	// All well-known channels are LISTENed up-front in Start(); Subscribe only
	// registers the in-memory subscriber. A channel outside that set cannot be
	// listened on after Start() — the listener loop holds the connection.
	if !eb.listening[channel] {
		slog.Warn("event bus: channel not pre-registered in Start() — "+
			"no notifications will be delivered for it", "channel", channel)
	}

	// Return cancel function that removes this subscriber.
	cancel := func() {
		close(sub.done)
		eb.mu.Lock()
		defer eb.mu.Unlock()
		subs := eb.subscribers[channel]
		for i, s := range subs {
			if s.ch == sub.ch {
				eb.subscribers[channel] = append(subs[:i], subs[i+1:]...)
				close(sub.ch)
				break
			}
		}
	}

	return sub.ch, cancel
}

// listenLoop waits for Postgres notifications and dispatches them to subscribers.
func (eb *PgEventBus) listenLoop(ctx context.Context) {
	defer close(eb.done)

	for {
		// WaitForNotification blocks until a notification arrives or ctx is cancelled.
		notification, err := eb.listenConn.WaitForNotification(ctx)
		if err != nil {
			// Context cancelled = normal shutdown.
			if ctx.Err() != nil {
				return
			}
			slog.Error("event bus: wait for notification failed", "error", err)
			return
		}

		event := Event{
			Channel: notification.Channel,
			Payload: json.RawMessage(notification.Payload),
		}

		eb.mu.Lock()
		subs := make([]subscriber, len(eb.subscribers[notification.Channel]))
		copy(subs, eb.subscribers[notification.Channel])
		eb.mu.Unlock()

		for _, sub := range subs {
			select {
			case <-sub.done:
				// Subscriber cancelled, skip.
			case sub.ch <- event:
				// Delivered.
			default:
				// Buffer full — drop the event to avoid blocking the listener loop.
				slog.Warn("event bus: subscriber buffer full, dropping event",
					"channel", notification.Channel)
			}
		}
	}
}

// MemoryEventBus is an in-memory EventBus for unit tests.
// No Postgres connection required.
type MemoryEventBus struct {
	mu          sync.Mutex
	subscribers map[string][]subscriber
	published   []Event // records all published events for assertions
}

// NewMemoryEventBus creates an in-memory event bus for testing.
func NewMemoryEventBus() *MemoryEventBus {
	return &MemoryEventBus{
		subscribers: make(map[string][]subscriber),
	}
}

// Publish delivers the event synchronously to all subscribers.
func (eb *MemoryEventBus) Publish(_ context.Context, channel string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("memory event bus: marshal: %w", err)
	}

	event := Event{
		Channel: channel,
		Payload: json.RawMessage(data),
	}

	eb.mu.Lock()
	eb.published = append(eb.published, event)
	subs := make([]subscriber, len(eb.subscribers[channel]))
	copy(subs, eb.subscribers[channel])
	eb.mu.Unlock()

	for _, sub := range subs {
		select {
		case <-sub.done:
		case sub.ch <- event:
		default:
		}
	}

	return nil
}

// Subscribe registers a listener for the given channel.
func (eb *MemoryEventBus) Subscribe(channel string) (_ <-chan Event, _ func()) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	sub := subscriber{
		ch:   make(chan Event, 16),
		done: make(chan struct{}),
	}
	eb.subscribers[channel] = append(eb.subscribers[channel], sub)

	cancel := func() {
		close(sub.done)
		eb.mu.Lock()
		defer eb.mu.Unlock()
		subs := eb.subscribers[channel]
		for i, s := range subs {
			if s.ch == sub.ch {
				eb.subscribers[channel] = append(subs[:i], subs[i+1:]...)
				close(sub.ch)
				break
			}
		}
	}

	return sub.ch, cancel
}

// Published returns all events published so far (for test assertions).
func (eb *MemoryEventBus) Published() []Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	result := make([]Event, len(eb.published))
	copy(result, eb.published)
	return result
}
