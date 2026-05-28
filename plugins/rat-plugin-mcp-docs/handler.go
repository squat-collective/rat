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
	platformToken string // per-startup random — advertised in Describe; ratd's proxy then injects it as X-RAT-Plugin-Token
}

func newHandler(name, platformToken string) *Handler {
	return &Handler{name: name, platformToken: platformToken}
}

func (h *Handler) HealthCheck(
	_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest],
) (*connect.Response[pluginv1.HealthCheckResponse], error) {
	return connect.NewResponse(&pluginv1.HealthCheckResponse{
		Status:  pluginv1.Status_STATUS_SERVING,
		Message: h.name + " ready",
	}), nil
}

// Describe advertises this plugin as a *server-side* MCP endpoint. There is
// no portal UI bundle — the chat plugin renders the catalog and "MCP servers"
// surface, and just consumes our /mcp endpoint over the interconnect.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	resp := sdk.NewDescribe(h.name, pluginVersion,
		"MCP server exposing RAT catalog & metadata (read-only) for AI tools").
		WithRoute("POST", "/mcp", "JSON-RPC 2.0 MCP endpoint (initialize, tools/list, tools/call)").
		WithRoute("GET", "/health", "Health probe").
		WithPlatformToken(h.platformToken).
		Build()
	return connect.NewResponse(resp), nil
}
