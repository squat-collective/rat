package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

const pluginVersion = "0.1.0"

// Handler implements RAT's PluginService for the docs-assistant plugin. It
// embeds UnimplementedPluginServiceHandler so RPCs it does not provide return
// CodeUnimplemented.
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

// Describe advertises the /suggest route and the portal UI — a button docked
// into the table-detail page via the "table-actions" slot. Clicking the
// button opens a modal that calls /suggest and lets the user save the
// generated documentation through the core table-metadata API.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "AI docs assistant — suggests a table description and per-column descriptions",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "POST", Path: "/suggest", Description: "Generate {description, column_descriptions} for a table"},
		},
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			Slots: []*pluginv1.UISlotDeclaration{
				{SlotId: "table-actions", ComponentName: "DocsAssistantButton", Priority: 50},
			},
		},
	}), nil
}
