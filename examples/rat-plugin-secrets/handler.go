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
		Description: "Encrypted secret store. AES-GCM at rest, key from RAT_SECRETS_KEY env (or /data/secrets.key fallback). Other plugins fetch named secrets via the interconnect 'secrets.get' capability.",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "GET", Path: "/secrets", Description: "List secrets (names + metadata only — never values)"},
			{Method: "POST", Path: "/secrets", Description: "Create or update a secret"},
			{Method: "DELETE", Path: "/secrets/{name}", Description: "Delete a secret"},
			{Method: "POST", Path: "/resolve", Description: "Resolve a name to its plaintext value (consumer plugins use this via the interconnect 'secrets.get' capability)"},
		},
		ConfigSchemaJson: configSchemaJSON,
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl: h.bundleURL,
			NavItems: []*pluginv1.UINavItem{
				{Label: "Secrets", Icon: "key", Path: "/x/secrets", Priority: 12},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/secrets", ComponentName: "SecretsApp"},
			},
		},
	}), nil
}
