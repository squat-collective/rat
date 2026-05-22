package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const pluginVersion = "0.1.0"

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

// Describe advertises the charts REST API and the portal UI (the Dashboards
// page). The route list is informational — ratd proxies /api/v1/x/charts/* as
// a wildcard, so every sub-path is reachable regardless.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "Charts, modular dashboards and reports — visualise your data",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "POST", Path: "/charts", Description: "Create a live chart"},
			{Method: "GET", Path: "/charts", Description: "List charts"},
			{Method: "GET", Path: "/charts/{id}/data", Description: "Run a chart's query and return its data"},
			{Method: "POST", Path: "/dashboards", Description: "Create a dashboard"},
			{Method: "GET", Path: "/dashboards", Description: "List dashboards"},
			{Method: "POST", Path: "/dashboards/{id}/widgets", Description: "Add a chart widget to a dashboard"},
			{Method: "POST", Path: "/reports", Description: "Create a report"},
			{Method: "GET", Path: "/reports", Description: "List reports"},
			{Method: "POST", Path: "/preview", Description: "Run ad-hoc SQL for the chart editor"},
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
