package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	sdk "github.com/rat-data/rat/sdk-go"
)

const pluginVersion = "0.1.0"

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
		Status: pluginv1.Status_STATUS_SERVING, Message: h.name + " ready",
	}), nil
}

func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	resp := sdk.NewDescribe(h.name, pluginVersion,
		"AI chat with pluggable MCP connectors — auto-discovers MCP servers wired through the interconnect and lets a model use them as tools").
		WithRoute("GET", "/servers", "Discovered MCP servers + tool catalogs").
		WithRoute("GET", "/tools", "Flattened, namespaced tool list as the LLM sees it").
		WithRoute("GET", "/agents", "Available agents (proxy of rat-plugin-agents) for the header picker").
		WithRoute("GET", "/conversations", "List persisted conversations (most-recent first)").
		WithRoute("GET", "/conversations/{id}", "Full conversation with messages").
		WithRoute("POST", "/conversations", "Create an empty conversation").
		WithRoute("PATCH", "/conversations/{id}", "Rename a conversation").
		WithRoute("DELETE", "/conversations/{id}", "Delete a conversation").
		WithRoute("GET", "/conversations/{id}/subagent-runs", "List subagent invocations triggered by this conversation").
		WithRoute("GET", "/subagent-runs/{id}", "Full event trace of one subagent invocation (for debugging hallucination etc.)").
		WithRoute("POST", "/conversations/{id}/continue", "Signal a paused chat (at its max_iterations cap) that the user said yes — keep going").
		WithRoute("POST", "/chat", "Run one chat turn (SSE: conversation / started / assistant_delta / assistant_message / tool_call / tool_result / done). Accepts conversation_id (server creates one if absent) + agent_id.").
		WithRoute("GET", "/config", "Effective chat config").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "Chat", Icon: "message-circle", Path: "/x/chat", Priority: 5}},
			[]*pluginv1.UIRoute{{Path: "/x/chat", ComponentName: "ChatApp"}}).
		WithPlatformToken(h.platformToken).
		WithConfigSchema(configSchemaJSON).
		Build()
	return connect.NewResponse(resp), nil
}
