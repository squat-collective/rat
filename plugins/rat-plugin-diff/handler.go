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
		"Activity feed of every change in the system. Polls ratd every 15s for plugins/configs, pipelines, schedules, secrets, namespaces, tables, and runs; emits structured events. Drill into any table-level event to see the row-level diff between two Iceberg snapshots.").
		WithRoute("GET", "/events", "Recent activity events (newest first). Query params: kind=<prefix>, limit=<n>").
		WithRoute("GET", "/tables/{ns}/{layer}/{name}/snapshots", "List Iceberg snapshots for a table (newest first)").
		WithRoute("POST", "/tables/{ns}/{layer}/{name}/diff", "Row-level diff between two snapshots. Body: {snapshot_a, snapshot_b, limit?}").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "Diff", Icon: "git-compare", Path: "/x/diff", Priority: 14}},
			[]*pluginv1.UIRoute{{Path: "/x/diff", ComponentName: "DiffApp"}}).
		WithPlatformToken(h.platformToken).
		Build()
	return connect.NewResponse(resp), nil
}
