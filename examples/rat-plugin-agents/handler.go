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
		Description: "Agent registry — named personas (system prompt + tool whitelist + model overrides) the chat plugin can switch between",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "GET", Path: "/agents", Description: "List all agents"},
			{Method: "GET", Path: "/agents/{id}", Description: "Get one agent"},
			{Method: "POST", Path: "/agents", Description: "Create an agent"},
			{Method: "PUT", Path: "/agents/{id}", Description: "Update an agent"},
			{Method: "DELETE", Path: "/agents/{id}", Description: "Delete an agent"},
			{Method: "POST", Path: "/agents/seed", Description: "Populate defaults if the catalog is empty"},
		},
		ConfigSchemaJson: configSchemaJSON,
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			NavItems: []*pluginv1.UINavItem{
				{Label: "Agents", Icon: "users", Path: "/x/agents", Priority: 6},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/agents", ComponentName: "AgentsApp"},
			},
		},
	}), nil
}
