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
		"Encrypted secret store. AES-GCM at rest, key from RAT_SECRETS_KEY env (or /data/secrets.key fallback). Other plugins fetch named secrets via the interconnect 'secrets.get' capability.").
		WithRoute("GET", "/secrets", "List secrets (names + metadata only — never values)").
		WithRoute("POST", "/secrets", "Create or update a secret").
		WithRoute("DELETE", "/secrets/{name}", "Delete a secret").
		WithRoute("POST", "/resolve", "Resolve a name to its plaintext value (consumer plugins use this via the interconnect 'secrets.get' capability)").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "Secrets", Icon: "key", Path: "/x/secrets", Priority: 12}},
			[]*pluginv1.UIRoute{{Path: "/x/secrets", ComponentName: "SecretsApp"}}).
		WithPlatformToken(h.platformToken).
		WithConfigSchema(configSchemaJSON).
		Build()
	return connect.NewResponse(resp), nil
}
