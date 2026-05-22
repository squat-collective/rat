package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const (
	pluginVersion = "0.1.0"
	maxEvents     = 50
)

// event is one platform event the notifier has observed.
type event struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// Handler implements RAT's PluginService for the event-notifier plugin.
//
// It embeds UnimplementedPluginServiceHandler so the auth/enforcement RPCs
// this plugin does not provide return CodeUnimplemented — ratd handles that
// gracefully (a plugin only implements what it needs).
type Handler struct {
	pluginv1connect.UnimplementedPluginServiceHandler

	name       string
	bundleURL  string
	webhookURL string

	mu     sync.Mutex
	events []event // most-recent-last, capped at maxEvents
}

func newHandler(name, bundleURL, webhookURL string) *Handler {
	return &Handler{name: name, bundleURL: bundleURL, webhookURL: webhookURL}
}

// HealthCheck reports that the plugin is ready to serve. ratd calls this on
// registration and then periodically via its health loop.
func (h *Handler) HealthCheck(
	_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest],
) (*connect.Response[pluginv1.HealthCheckResponse], error) {
	return connect.NewResponse(&pluginv1.HealthCheckResponse{
		Status:  pluginv1.Status_STATUS_SERVING,
		Message: h.name + " ready",
	}), nil
}

// Describe tells ratd what this plugin is: which events to deliver to it, the
// HTTP routes it exposes (proxied under /api/v1/x/{name}/...), and how it
// integrates with the portal UI.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:               h.name,
		Version:            pluginVersion,
		Description:        "Records platform events and optionally forwards them to a webhook",
		EventSubscriptions: []string{"run_completed", "quality_failed"},
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "GET", Path: "/events", Description: "Recent platform events seen by the notifier"},
		},
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			Slots: []*pluginv1.UISlotDeclaration{
				{SlotId: "dashboard-widgets", ComponentName: "EventNotifierWidget", Priority: 50},
			},
			NavItems: []*pluginv1.UINavItem{
				{Label: "Events", Icon: "bell", Path: "/x/event-notifier", Priority: 50},
			},
		},
	}), nil
}

// HandleEvent receives a platform event ratd delivers because this plugin
// subscribed to it in Describe. The event is recorded and, when WEBHOOK_URL
// is configured, forwarded to that webhook.
func (h *Handler) HandleEvent(
	ctx context.Context, req *connect.Request[pluginv1.HandleEventRequest],
) (*connect.Response[pluginv1.HandleEventResponse], error) {
	msg := req.Msg
	ev := event{
		Type:      msg.EventType,
		ID:        msg.EventId,
		Timestamp: msg.Timestamp,
		Payload:   json.RawMessage(msg.Payload),
	}
	h.record(ev)
	slog.Info("event received", "type", ev.Type, "id", ev.ID)

	if h.webhookURL != "" {
		h.forward(ctx, ev)
	}
	return connect.NewResponse(&pluginv1.HandleEventResponse{}), nil
}

func (h *Handler) record(ev event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, ev)
	if len(h.events) > maxEvents {
		h.events = h.events[len(h.events)-maxEvents:]
	}
}

func (h *Handler) recentEvents() []event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]event, len(h.events))
	copy(out, h.events)
	return out
}

func (h *Handler) forward(ctx context.Context, ev event) {
	body, _ := json.Marshal(ev)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, h.webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("webhook request build failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("webhook delivery failed", "error", err)
		return
	}
	_ = resp.Body.Close()
}

// ServeEvents handles GET /events — ratd proxies it at
// /api/v1/x/event-notifier/events. Returns the recent events as JSON.
func (h *Handler) ServeEvents(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.recentEvents())
}
