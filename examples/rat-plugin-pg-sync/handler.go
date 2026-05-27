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
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:        h.name,
		Version:     pluginVersion,
		Description: "Mirror external Postgres tables into RAT's Iceberg lake. Connection URLs are stored as named secrets (via rat-plugin-secrets); each table sync becomes an auto-generated SQL pipeline + cron schedule. Modes: snapshot (full refresh) or incremental (watermark-filtered append).",
		Routes: []*pluginv1.RouteDeclaration{
			{Method: "GET", Path: "/connections", Description: "List configured connections"},
			{Method: "POST", Path: "/connections", Description: "Create or update a connection"},
			{Method: "DELETE", Path: "/connections/{name}", Description: "Delete a connection (cascades to its table syncs)"},
			{Method: "POST", Path: "/connections/{name}/test", Description: "Verify the secret resolves"},
			{Method: "GET", Path: "/tables", Description: "List all table syncs"},
			{Method: "POST", Path: "/tables", Description: "Add a table sync — generates pipeline + schedule"},
			{Method: "PUT", Path: "/tables/{id}", Description: "Update an existing table sync"},
			{Method: "DELETE", Path: "/tables/{id}", Description: "Remove a table sync (tears down pipeline + schedule)"},
			{Method: "POST", Path: "/tables/{id}/sync-now", Description: "Trigger an immediate run"},
		},
		PlatformToken: h.platformToken,
		Ui: &pluginv1.PluginUIDescriptor{
			BundleUrl:  h.bundleURL,
			BundleHash: h.bundleHash,
			NavItems: []*pluginv1.UINavItem{
				{Label: "Pg Sync", Icon: "database", Path: "/x/pg-sync", Priority: 13},
			},
			Routes: []*pluginv1.UIRoute{
				{Path: "/x/pg-sync", ComponentName: "PgSyncApp"},
			},
		},
	}), nil
}
