// Package query provides the gRPC client for the ratq query service.
// It implements the api.QueryStore interface by proxying to the Python DuckDB sidecar.
package query

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	queryv1 "github.com/rat-data/rat/platform/gen/query/v1"
	"github.com/rat-data/rat/platform/gen/query/v1/queryv1connect"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/arrowutil"
)

// Client implements api.QueryStore by proxying to ratq via ConnectRPC.
type Client struct {
	rpc queryv1connect.QueryServiceClient
}

// NewClient creates a query client that talks to ratq at the given address.
// The caller must provide an HTTP client (typically the shared gRPC transport
// from transport.NewGRPCClient) instead of creating its own. This ensures
// consistent TLS/h2c configuration across all gRPC clients.
func NewClient(addr string, httpClient *http.Client) *Client {
	return &Client{
		rpc: queryv1connect.NewQueryServiceClient(httpClient, addr, connect.WithGRPC()),
	}
}

// newClientWithRPC creates a client with an injected RPC client (for testing).
func newClientWithRPC(rpc queryv1connect.QueryServiceClient) *Client {
	return &Client{rpc: rpc}
}

// propagateRequestID copies the request ID from the context into the ConnectRPC
// request header so ratq can correlate logs back to the original HTTP request.
func propagateRequestID[T any](ctx context.Context, req *connect.Request[T]) {
	if id := api.RequestIDFromContext(ctx); id != "" {
		req.Header().Set("X-Request-ID", id)
	}
}

// ExecuteQuery runs a SQL query via ratq and returns JSON-serializable results.
func (c *Client) ExecuteQuery(ctx context.Context, sql string, namespace string, limit int) (*api.QueryResult, error) {
	req := connect.NewRequest(&queryv1.ExecuteQueryRequest{
		Sql:       sql,
		Namespace: namespace,
		Limit:     int32(limit),
	})
	propagateRequestID(ctx, req)
	resp, err := c.rpc.ExecuteQuery(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}

	columns := protoColumnsToAPI(resp.Msg.Columns)
	rows, err := arrowutil.IPCToRows(resp.Msg.ArrowBatch)
	if err != nil {
		return nil, fmt.Errorf("deserialize arrow: %w", err)
	}

	return &api.QueryResult{
		Columns:    columns,
		Rows:       rows,
		TotalRows:  int(resp.Msg.TotalRows),
		DurationMs: int64(resp.Msg.DurationMs),
	}, nil
}

// ListTables returns all tables, optionally filtered by layer.
func (c *Client) ListTables(ctx context.Context, namespace, layer string) ([]api.TableInfo, error) {
	req := connect.NewRequest(&queryv1.ListTablesRequest{
		Namespace: namespace,
		Layer:     stringToProtoLayer(layer),
	})
	propagateRequestID(ctx, req)
	resp, err := c.rpc.ListTables(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	tables := make([]api.TableInfo, 0, len(resp.Msg.Tables))
	for _, t := range resp.Msg.Tables {
		tables = append(tables, api.TableInfo{
			Namespace: t.Namespace,
			Layer:     protoLayerToString(t.Layer),
			Name:      t.Name,
			RowCount:  t.RowCount,
			SizeBytes: t.SizeBytes,
		})
	}
	return tables, nil
}

// GetTable returns schema and stats for a single table.
func (c *Client) GetTable(ctx context.Context, namespace, layer, name string) (*api.TableDetail, error) {
	req := connect.NewRequest(&queryv1.GetSchemaRequest{
		Namespace: namespace,
		Layer:     stringToProtoLayer(layer),
		TableName: name,
	})
	propagateRequestID(ctx, req)
	resp, err := c.rpc.GetSchema(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get table: %w", err)
	}

	columns := protoColumnsToAPI(resp.Msg.Columns)
	return &api.TableDetail{
		TableInfo: api.TableInfo{
			Namespace: namespace,
			Layer:     layer,
			Name:      name,
			RowCount:  resp.Msg.RowCount,
			SizeBytes: resp.Msg.SizeBytes,
		},
		Columns: columns,
	}, nil
}

// GetBulkTableSchemas returns all tables with their column schemas in a single operation.
// It lists all tables then fetches each schema concurrently with bounded parallelism.
// This replaces the N+1 pattern of ListTables + N*GetTable with a single bulk call.
// When the ratq sidecar adds a native bulk RPC, this can be updated to use it.
//
// A derived context with a 60-second timeout is used for the concurrent goroutines
// rather than sharing the request context directly. The request context may be
// cancelled when the HTTP handler returns, causing in-flight gRPC calls to fail
// with spurious errors.
func (c *Client) GetBulkTableSchemas(ctx context.Context) ([]api.SchemaEntry, error) {
	tables, err := c.ListTables(ctx, "", "")
	if err != nil {
		return nil, fmt.Errorf("bulk schemas: list tables: %w", err)
	}

	entries := make([]api.SchemaEntry, len(tables))
	sem := make(chan struct{}, 10) // bounded concurrency
	var wg sync.WaitGroup

	// Derive a context with a timeout for goroutines. This decouples the
	// concurrent schema fetches from the HTTP request lifecycle while still
	// respecting cancellation from the caller.
	fetchCtx, fetchCancel := context.WithTimeout(ctx, 60*time.Second)
	defer fetchCancel()

	for i, t := range tables {
		wg.Add(1)
		go func(idx int, ti api.TableInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			detail, err := c.GetTable(fetchCtx, ti.Namespace, ti.Layer, ti.Name)
			if err != nil || detail == nil {
				entries[idx] = api.SchemaEntry{
					Namespace: ti.Namespace,
					Layer:     ti.Layer,
					Name:      ti.Name,
					Columns:   []api.QueryColumn{},
				}
				return
			}
			entries[idx] = api.SchemaEntry{
				Namespace: ti.Namespace,
				Layer:     ti.Layer,
				Name:      ti.Name,
				Columns:   detail.Columns,
			}
		}(i, t)
	}
	wg.Wait()

	return entries, nil
}

// PreviewTable returns the first N rows of a table.
func (c *Client) PreviewTable(ctx context.Context, namespace, layer, name string, limit int) (*api.QueryResult, error) {
	req := connect.NewRequest(&queryv1.PreviewTableRequest{
		Namespace: namespace,
		Layer:     stringToProtoLayer(layer),
		TableName: name,
		Limit:     int32(limit),
	})
	propagateRequestID(ctx, req)
	resp, err := c.rpc.PreviewTable(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("preview table: %w", err)
	}

	columns := protoColumnsToAPI(resp.Msg.Columns)
	rows, err := arrowutil.IPCToRows(resp.Msg.ArrowBatch)
	if err != nil {
		return nil, fmt.Errorf("deserialize arrow: %w", err)
	}

	return &api.QueryResult{
		Columns:    columns,
		Rows:       rows,
		TotalRows:  len(rows),
		DurationMs: 0,
	}, nil
}

// --- Helpers ---

func protoColumnsToAPI(cols []*queryv1.ColumnMeta) []api.QueryColumn {
	result := make([]api.QueryColumn, 0, len(cols))
	for _, c := range cols {
		result = append(result, api.QueryColumn{
			Name: c.Name,
			Type: c.Type,
		})
	}
	return result
}

func stringToProtoLayer(layer string) commonv1.Layer {
	switch layer {
	case "bronze":
		return commonv1.Layer_LAYER_BRONZE
	case "silver":
		return commonv1.Layer_LAYER_SILVER
	case "gold":
		return commonv1.Layer_LAYER_GOLD
	default:
		return commonv1.Layer_LAYER_UNSPECIFIED
	}
}

func protoLayerToString(layer commonv1.Layer) string {
	switch layer {
	case commonv1.Layer_LAYER_BRONZE:
		return "bronze"
	case commonv1.Layer_LAYER_SILVER:
		return "silver"
	case commonv1.Layer_LAYER_GOLD:
		return "gold"
	default:
		return ""
	}
}
