package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const pluginVersion = "0.1.0"

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

	name          string
	bundleURL     string
	bundleHash    string // SRI format ("sha256-<base64>") — surfaced in Describe so the portal can set <script integrity>
	platformToken string // per-startup random — advertised in Describe; ratd's proxy then injects it as X-RAT-Plugin-Token
	cfg           *configStore

	mu     sync.Mutex
	events []event // most-recent-last, capped at the configured max
}

func newHandler(name, bundleURL, bundleHash, platformToken string, cfg *configStore) *Handler {
	return &Handler{name: name, bundleURL: bundleURL, bundleHash: bundleHash, platformToken: platformToken, cfg: cfg}
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
// HTTP routes it exposes (proxied under /api/v1/x/{name}/...), how it
// integrates with the portal UI, and the JSON Schema for its configuration
// (which the portal renders as a settings form).
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
		ConfigSchemaJson: configSchemaJSON,
		PlatformToken:    h.platformToken,
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl:  h.bundleURL,
			BundleHash: h.bundleHash,
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
// subscribed to it in Describe. The event is recorded and, when a webhook is
// configured, forwarded to it — subject to the "forward only failures" setting.
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

	c := h.cfg.get()
	if c.WebhookURL != "" && (!c.ForwardOnlyFailures || isFailureEvent(ev)) {
		h.forward(ctx, ev, c.WebhookURL)
	}
	return connect.NewResponse(&pluginv1.HandleEventResponse{}), nil
}

// record appends an event, capping the buffer at the configured size.
func (h *Handler) record(ev event) {
	max := h.cfg.get().MaxEvents
	if max <= 0 {
		max = defaultMaxEvents
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, ev)
	if len(h.events) > max {
		h.events = h.events[len(h.events)-max:]
	}
}

func (h *Handler) recentEvents() []event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]event, len(h.events))
	copy(out, h.events)
	return out
}

// isFailureEvent reports whether an event represents a failure — a
// quality_failed event, or a run_completed whose payload carries a failed
// status. Used by the "forward only failures" setting.
func isFailureEvent(ev event) bool {
	if ev.Type == "quality_failed" {
		return true
	}
	if len(ev.Payload) == 0 {
		return false
	}
	var m map[string]any
	if json.Unmarshal(ev.Payload, &m) != nil {
		return false
	}
	for _, key := range []string{"status", "state", "outcome", "result"} {
		if s, ok := m[key].(string); ok {
			ls := strings.ToLower(s)
			if strings.Contains(ls, "fail") || strings.Contains(ls, "error") {
				return true
			}
		}
	}
	return false
}

// forward POSTs an event as JSON to the configured webhook.
func (h *Handler) forward(ctx context.Context, ev event, webhookURL string) {
	body, _ := json.Marshal(ev)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, webhookURL, bytes.NewReader(body))
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
