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
		Status: pluginv1.Status_STATUS_SERVING, Message: h.name + " ready",
	}), nil
}

func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	resp := sdk.NewDescribe(h.name, pluginVersion,
		"Agent registry — named personas (system prompt + tool whitelist + model overrides) the chat plugin can switch between").
		WithRoute("GET", "/agents", "List all agents").
		WithRoute("GET", "/agents/{id}", "Get one agent").
		WithRoute("POST", "/agents", "Create an agent").
		WithRoute("PUT", "/agents/{id}", "Update an agent").
		WithRoute("DELETE", "/agents/{id}", "Delete an agent").
		WithRoute("POST", "/agents/seed", "Populate defaults if the catalog is empty").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "Agents", Icon: "users", Path: "/x/agents", Priority: 6}},
			[]*pluginv1.UIRoute{{Path: "/x/agents", ComponentName: "AgentsApp"}}).
		WithPlatformToken(h.platformToken).
		WithConfigSchema(configSchemaJSON).
		Build()
	return connect.NewResponse(resp), nil
}
