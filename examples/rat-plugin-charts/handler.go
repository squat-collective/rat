package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const pluginVersion = "0.2.0"

// Handler implements RAT's PluginService for the charts plugin. It embeds
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

// Describe advertises the dashboards REST API and the portal UI. The route
// list is informational — ratd proxies /api/v1/x/charts/* as a wildcard, so
// every sub-path is reachable regardless.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "Living dashboards — a grid of charts, text, metrics and AI insights",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "POST", Path: "/dashboards", Description: "Create a dashboard"},
			{Method: "GET", Path: "/dashboards", Description: "List dashboards"},
			{Method: "GET", Path: "/dashboards/{id}", Description: "Get a dashboard and its components"},
			{Method: "PATCH", Path: "/dashboards/{id}", Description: "Update a dashboard's title or components"},
			{Method: "POST", Path: "/dashboards/{id}/components", Description: "Add a component to a dashboard"},
			{Method: "POST", Path: "/query", Description: "Run a read-only SQL query for a component"},
		},
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			NavItems: []*pluginv1.UINavItem{
				{Label: "Dashboards", Icon: "layout-dashboard", Path: "/x/charts", Priority: 20},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/charts", ComponentName: "ChartsApp"},
			},
		},
	}), nil
}
