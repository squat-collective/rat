package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
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
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "Plugin interconnection — a capability registry, a broker, and a live plugin mesh",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "GET", Path: "/mesh", Description: "The live plugin mesh: plugins + capabilities"},
			{Method: "GET", Path: "/capabilities", Description: "List registered capabilities"},
			{Method: "POST", Path: "/register", Description: "Register a capability a plugin offers"},
			{Method: "POST", Path: "/invoke", Description: "Invoke a capability by name — the broker routes it"},
		},
		PlatformToken: h.platformToken,
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl:  h.bundleURL,
			BundleHash: h.bundleHash,
			NavItems: []*pluginv1.UINavItem{
				{Label: "Plugin Mesh", Icon: "network", Path: "/x/interconnect", Priority: 30},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/interconnect", ComponentName: "InterconnectApp"},
			},
		},
	}), nil
}
