package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const pluginVersion = "0.1.0"

// Handler implements RAT's PluginService for the AI plugin. It embeds
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

// Describe advertises the /chat route and the portal UI (the chat page).
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "AI data navigator — explore and analyse your data through chat",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "POST", Path: "/chat", Description: "Send a message to the data navigator"},
		},
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			NavItems: []*pluginv1.UINavItem{
				{Label: "AI Assistant", Icon: "sparkles", Path: "/x/ai", Priority: 10},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/ai", ComponentName: "AIChatPage"},
			},
		},
	}), nil
}
