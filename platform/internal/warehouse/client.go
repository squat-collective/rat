// Package warehouse provides ratd's ConnectRPC client for the active warehouse
// plugin (ADR-024). It implements api.WarehouseClient + api.HealthChecker by
// proxying to the warehouse sidecar that serves warehouse/v1.
package warehouse

import (
	"context"
	"net/http"

	connect "connectrpc.com/connect"
	warehousev1 "github.com/rat-data/rat/platform/gen/warehouse/v1"
	"github.com/rat-data/rat/platform/gen/warehouse/v1/warehousev1connect"
	"github.com/rat-data/rat/platform/internal/api"
)

// Client talks to the warehouse plugin over ConnectRPC.
type Client struct {
	rpc warehousev1connect.WarehouseServiceClient
}

// NewClient creates a warehouse client for the plugin at addr. The caller passes
// the shared gRPC transport (transport.NewGRPCClient) so TLS/h2c config matches
// every other gRPC client in ratd.
func NewClient(addr string, httpClient *http.Client) *Client {
	return &Client{
		rpc: warehousev1connect.NewWarehouseServiceClient(httpClient, addr, connect.WithGRPC()),
	}
}

// newClientWithRPC injects an RPC client (for testing).
func newClientWithRPC(rpc warehousev1connect.WarehouseServiceClient) *Client {
	return &Client{rpc: rpc}
}

// Describe returns the warehouse's identity + advertised capabilities.
func (c *Client) Describe(ctx context.Context) (api.WarehouseInfo, error) {
	resp, err := c.rpc.Describe(ctx, connect.NewRequest(&warehousev1.DescribeRequest{}))
	if err != nil {
		return api.WarehouseInfo{}, err
	}
	caps := make([]string, 0, len(resp.Msg.GetCapabilities()))
	for _, capability := range resp.Msg.GetCapabilities() {
		caps = append(caps, capability.String())
	}
	return api.WarehouseInfo{
		Name:         resp.Msg.GetName(),
		Version:      resp.Msg.GetVersion(),
		Capabilities: caps,
	}, nil
}

// HealthCheck implements api.HealthChecker: a Describe round-trip proves the
// warehouse is reachable and speaking warehouse/v1.
func (c *Client) HealthCheck(ctx context.Context) error {
	_, err := c.rpc.Describe(ctx, connect.NewRequest(&warehousev1.DescribeRequest{}))
	return err
}
