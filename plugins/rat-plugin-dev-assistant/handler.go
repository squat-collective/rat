package main

import (
	"context"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	sdk "github.com/rat-data/rat/sdk-go"
)

const pluginVersion = "0.1.0"

// Handler implements RAT's PluginService for the dev-assistant plugin. It
// embeds UnimplementedPluginServiceHandler so RPCs it does not provide return
// CodeUnimplemented.
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

// Describe advertises the /chat route and the portal UI — a panel docked into
// the pipeline editor via the "pipeline-editor-sidebar" slot.
func (h *Handler) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	resp := sdk.NewDescribe(h.name, pluginVersion,
		"AI dev assistant — writes, explains and fixes pipeline code in the editor").
		WithRoute("POST", "/chat", "Ask the dev assistant; brokered to the AI provider").
		WithPlatformToken(h.platformToken).
		Build()
	resp.Ui = &pluginv1.PluginUIDescriptor{
		BundleUrl:  h.bundleURL,
		BundleHash: h.bundleHash,
		Slots: []*pluginv1.UISlotDeclaration{
			{SlotId: "pipeline-editor-sidebar", ComponentName: "DevAssistantPanel", Priority: 50},
		},
	}
	return connect.NewResponse(resp), nil
}
