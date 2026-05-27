package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	sdk "github.com/rat-data/rat/sdk-go"
)

const pluginVersion = "0.1.0"

// Handler implements RAT's PluginService for the interconnect plugin. It embeds
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

// Describe advertises the interconnect REST API and the portal UI (the Plugin
// Mesh page). The route list is informational — ratd proxies
// /api/v1/x/interconnect/* as a wildcard.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	resp := sdk.NewDescribe(h.name, pluginVersion,
		"Plugin interconnection — a capability registry, a broker, and a live plugin mesh").
		WithRoute("GET", "/mesh", "The live plugin mesh: plugins + capabilities").
		WithRoute("GET", "/capabilities", "List registered capabilities").
		WithRoute("POST", "/register", "Register a capability a plugin offers").
		WithRoute("POST", "/invoke", "Invoke a capability by name — the broker routes it").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "Plugin Mesh", Icon: "network", Path: "/x/interconnect", Priority: 30}},
			[]*pluginv1.UIRoute{{Path: "/x/interconnect", ComponentName: "InterconnectApp"}}).
		WithPlatformToken(h.platformToken).
		Build()
	return connect.NewResponse(resp), nil
}
