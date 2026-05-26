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
		Status:  pluginv1.Status_STATUS_SERVING,
		Message: h.name + " ready",
	}), nil
}

func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "Pipeline lineage DAG — visualises the dependency graph of pipelines, tables and landing zones. Migrated out of core into a plugin.",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "GET", Path: "/graph", Description: "Build and return the lineage DAG. Optional ?namespace=… filter."},
			{Method: "GET", Path: "/health", Description: "Health probe"},
		},
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			NavItems: []*pluginv1.UINavItem{
				{Label: "Lineage", Icon: "git-branch", Path: "/x/lineage", Priority: 15},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/lineage", ComponentName: "LineageApp"},
			},
		},
	}), nil
}
