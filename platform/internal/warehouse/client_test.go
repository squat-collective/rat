package warehouse

import (
	"context"
	"errors"
	"testing"

	connect "connectrpc.com/connect"
	warehousev1 "github.com/rat-data/rat/platform/gen/warehouse/v1"
	"github.com/rat-data/rat/platform/gen/warehouse/v1/warehousev1connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRPC embeds the generated client interface so only Describe needs an impl;
// any other RPC the test doesn't exercise would panic (intentionally unused).
type fakeRPC struct {
	warehousev1connect.WarehouseServiceClient
	resp *warehousev1.DescribeResponse
	err  error
}

func (f *fakeRPC) Describe(
	_ context.Context, _ *connect.Request[warehousev1.DescribeRequest],
) (*connect.Response[warehousev1.DescribeResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(f.resp), nil
}

func TestClient_Describe_MapsCapabilities(t *testing.T) {
	c := newClientWithRPC(&fakeRPC{resp: &warehousev1.DescribeResponse{
		Name:    "iceberg-nessie",
		Version: "0.2.0b1",
		Capabilities: []warehousev1.Capability{
			warehousev1.Capability_CAPABILITY_BRANCHING,
			warehousev1.Capability_CAPABILITY_TIME_TRAVEL,
		},
	}})

	info, err := c.Describe(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "iceberg-nessie", info.Name)
	assert.Equal(t, "0.2.0b1", info.Version)
	assert.Equal(t, []string{"CAPABILITY_BRANCHING", "CAPABILITY_TIME_TRAVEL"}, info.Capabilities)
}

func TestClient_HealthCheck_PropagatesError(t *testing.T) {
	c := newClientWithRPC(&fakeRPC{err: errors.New("warehouse down")})
	assert.Error(t, c.HealthCheck(context.Background()))
}

func TestClient_HealthCheck_OK(t *testing.T) {
	c := newClientWithRPC(&fakeRPC{resp: &warehousev1.DescribeResponse{Name: "iceberg-nessie"}})
	assert.NoError(t, c.HealthCheck(context.Background()))
}
