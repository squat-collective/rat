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
	bundleHash    string
	platformToken string
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
		"Detects Iceberg tables with too many small files and compacts them via PyIceberg's overwrite() workaround. Runs on a periodic loop; manual compaction is available per-table via the UI. Counts files in S3, computes small_file_ratio, and rewrites when the ratio exceeds a configurable threshold.").
		WithRoute("GET", "/tables", "List monitored Iceberg tables + their file-health stats").
		WithRoute("POST", "/tables/{namespace}/{layer}/{name}/compact", "Trigger an immediate compaction of one table").
		WithRoute("POST", "/scan", "Force an out-of-cycle detection sweep").
		WithUI(h.bundleURL, h.bundleHash,
			[]*pluginv1.UINavItem{{Label: "Compaction", Icon: "boxes", Path: "/x/compaction", Priority: 14}},
			[]*pluginv1.UIRoute{{Path: "/x/compaction", ComponentName: "CompactionApp"}}).
		WithPlatformToken(h.platformToken).
		Build()
	return connect.NewResponse(resp), nil
}
