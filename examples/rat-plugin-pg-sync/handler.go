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
		"Mirror external Postgres tables into RAT's Iceberg lake. Connection URLs are stored as named secrets (via rat-plugin-secrets); each table sync becomes an auto-generated SQL pipeline + cron schedule. Modes: snapshot (full refresh) or incremental (watermark-filtered append).").
		WithRoute("GET", "/connections", "List configured connections").
		WithRoute("POST", "/connections", "Create or update a connection").
		WithRoute("DELETE", "/connections/{name}", "Delete a connection (cascades to its table syncs)").
		WithRoute("POST", "/connections/{name}/test", "Verify the secret resolves").
		WithRoute("GET", "/tables", "List all table syncs").
		WithRoute("POST", "/tables", "Add a table sync — generates pipeline + schedule").
		WithRoute("PUT", "/tables/{id}", "Update an existing table sync").
		WithRoute("DELETE", "/tables/{id}", "Remove a table sync (tears down pipeline + schedule)").
		WithRoute("POST", "/tables/{id}/sync-now", "Trigger an immediate run").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "Pg Sync", Icon: "database", Path: "/x/pg-sync", Priority: 13}},
			[]*pluginv1.UIRoute{{Path: "/x/pg-sync", ComponentName: "PgSyncApp"}}).
		WithPlatformToken(h.platformToken).
		Build()
	return connect.NewResponse(resp), nil
}
