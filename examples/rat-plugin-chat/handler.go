package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const pluginVersion = "0.1.0"

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
		Status: pluginv1.Status_STATUS_SERVING, Message: h.name + " ready",
	}), nil
}

func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "AI chat with pluggable MCP connectors — auto-discovers MCP servers wired through the interconnect and lets a model use them as tools",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "GET", Path: "/servers", Description: "Discovered MCP servers + tool catalogs"},
			{Method: "GET", Path: "/tools", Description: "Flattened, namespaced tool list as the LLM sees it"},
			{Method: "GET", Path: "/agents", Description: "Available agents (proxy of rat-plugin-agents) for the header picker"},
			{Method: "GET", Path: "/conversations", Description: "List persisted conversations (most-recent first)"},
			{Method: "GET", Path: "/conversations/{id}", Description: "Full conversation with messages"},
			{Method: "POST", Path: "/conversations", Description: "Create an empty conversation"},
			{Method: "PATCH", Path: "/conversations/{id}", Description: "Rename a conversation"},
			{Method: "DELETE", Path: "/conversations/{id}", Description: "Delete a conversation"},
			{Method: "POST", Path: "/chat", Description: "Run one chat turn (SSE: conversation / started / assistant_delta / assistant_message / tool_call / tool_result / done). Accepts conversation_id (server creates one if absent) + agent_id."},
			{Method: "GET", Path: "/config", Description: "Effective chat config"},
		},
		ConfigSchemaJson: configSchemaJSON,
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			NavItems: []*pluginv1.UINavItem{
				{Label: "Chat", Icon: "message-circle", Path: "/x/chat", Priority: 5},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/chat", ComponentName: "ChatApp"},
			},
		},
	}), nil
}
