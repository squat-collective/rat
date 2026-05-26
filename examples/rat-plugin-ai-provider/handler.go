package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const pluginVersion = "0.1.0"

// Handler implements RAT's PluginService for the AI provider plugin. It embeds
// UnimplementedPluginServiceHandler so RPCs it does not provide (HandleEvent,
// Authenticate, Authorize) return CodeUnimplemented.
type Handler struct {
	pluginv1connect.UnimplementedPluginServiceHandler

	name      string
	bundleURL string
}

func newHandler(name, bundleURL string) *Handler {
	return &Handler{name: name, bundleURL: bundleURL}
}

func (h *Handler) HealthCheck(
	_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest],
) (*connect.Response[pluginv1.HealthCheckResponse], error) {
	return connect.NewResponse(&pluginv1.HealthCheckResponse{
		Status:  pluginv1.Status_STATUS_SERVING,
		Message: h.name + " ready",
	}), nil
}

// Describe advertises the provider API, the portal UI, and — crucially — a
// config_schema_json. The portal's plugin-config editor renders a settings
// form straight from that schema, which is what makes this plugin
// configurable from the portal.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "AI provider — a configurable, reusable OpenAI-compatible LLM service for other plugins",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "POST", Path: "/complete", Description: "One-shot LLM completion (prompt in, text out)"},
			{Method: "POST", Path: "/chat", Description: "Raw multi-message chat completion"},
			{Method: "POST", Path: "/chat-with-tools", Description: "Chat with OpenAI-style tool/function calling (returns tool_calls + finish_reason)"},
			{Method: "POST", Path: "/chat-with-tools-stream", Description: "Streaming variant — SSE events 'delta' / 'done' / 'error'"},
			{Method: "GET", Path: "/config", Description: "The current effective config (API key masked)"},
		},
		ConfigSchemaJson: configSchemaJSON,
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			NavItems: []*pluginv1.UINavItem{
				{Label: "AI Provider", Icon: "sparkles", Path: "/x/ai-provider", Priority: 40},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/ai-provider", ComponentName: "AIProviderApp"},
			},
		},
	}), nil
}
