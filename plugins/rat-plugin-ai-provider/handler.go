package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	sdk "github.com/rat-data/rat/sdk-go"
)

const pluginVersion = "0.1.0"

// Handler implements RAT's PluginService for the AI provider plugin. It embeds
// UnimplementedPluginServiceHandler so RPCs it does not provide (HandleEvent,
// Authenticate, Authorize) return CodeUnimplemented.
type Handler struct {
	pluginv1connect.UnimplementedPluginServiceHandler

	name          string
	bundleURL     string
	bundleHash    string // SRI format ("sha256-<base64>") — surfaced in Describe so the portal can set <script integrity>
	platformToken string // per-startup random — advertised in Describe; ratd's proxy then injects it as X-RAT-Plugin-Token
}

func newHandler(name, bundleURL, bundleHash, platformToken string) *Handler {
	return &Handler{name: name, bundleURL: bundleURL, bundleHash: bundleHash, platformToken: platformToken}
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
	resp := sdk.NewDescribe(h.name, pluginVersion,
		"AI provider — a configurable, reusable OpenAI-compatible LLM service for other plugins").
		WithRoute("POST", "/complete", "One-shot LLM completion (prompt in, text out)").
		WithRoute("POST", "/chat", "Raw multi-message chat completion").
		WithRoute("POST", "/chat-with-tools", "Chat with OpenAI-style tool/function calling (returns tool_calls + finish_reason)").
		WithRoute("POST", "/chat-with-tools-stream", "Streaming variant — SSE events 'delta' / 'done' / 'error'").
		WithRoute("GET", "/config", "The current effective config (API key masked)").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "AI Provider", Icon: "sparkles", Path: "/x/ai-provider", Priority: 40}},
			[]*pluginv1.UIRoute{{Path: "/x/ai-provider", ComponentName: "AIProviderApp"}}).
		WithPlatformToken(h.platformToken).
		WithConfigSchema(configSchemaJSON).
		Build()
	return connect.NewResponse(resp), nil
}
