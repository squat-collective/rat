package query

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	queryv1 "github.com/rat-data/rat/platform/gen/query/v1"
	"github.com/rat-data/rat/platform/internal/arrowutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock query client ---

type mockQueryServiceClient struct {
	executeQueryFunc func(ctx context.Context, req *connect.Request[queryv1.ExecuteQueryRequest]) (*connect.Response[queryv1.ExecuteQueryResponse], error)
	getSchemaFunc    func(ctx context.Context, req *connect.Request[queryv1.GetSchemaRequest]) (*connect.Response[queryv1.GetSchemaResponse], error)
	previewFunc      func(ctx context.Context, req *connect.Request[queryv1.PreviewTableRequest]) (*connect.Response[queryv1.PreviewTableResponse], error)
	listTablesFunc   func(ctx context.Context, req *connect.Request[queryv1.ListTablesRequest]) (*connect.Response[queryv1.ListTablesResponse], error)
}

func (m *mockQueryServiceClient) ExecuteQuery(ctx context.Context, req *connect.Request[queryv1.ExecuteQueryRequest]) (*connect.Response[queryv1.ExecuteQueryResponse], error) {
	if m.executeQueryFunc != nil {
		return m.executeQueryFunc(ctx, req)
	}
	return connect.NewResponse(&queryv1.ExecuteQueryResponse{}), nil
}

func (m *mockQueryServiceClient) GetSchema(ctx context.Context, req *connect.Request[queryv1.GetSchemaRequest]) (*connect.Response[queryv1.GetSchemaResponse], error) {
	if m.getSchemaFunc != nil {
		return m.getSchemaFunc(ctx, req)
	}
	return connect.NewResponse(&queryv1.GetSchemaResponse{}), nil
}

func (m *mockQueryServiceClient) PreviewTable(ctx context.Context, req *connect.Request[queryv1.PreviewTableRequest]) (*connect.Response[queryv1.PreviewTableResponse], error) {
	if m.previewFunc != nil {
		return m.previewFunc(ctx, req)
	}
	return connect.NewResponse(&queryv1.PreviewTableResponse{}), nil
}

func (m *mockQueryServiceClient) ListTables(ctx context.Context, req *connect.Request[queryv1.ListTablesRequest]) (*connect.Response[queryv1.ListTablesResponse], error) {
	if m.listTablesFunc != nil {
		return m.listTablesFunc(ctx, req)
	}
	return connect.NewResponse(&queryv1.ListTablesResponse{}), nil
}

// --- Test helpers ---

// buildArrowIPC creates Arrow IPC bytes from simple column data (for testing).
func buildArrowIPC(names []string, values [][]interface{}) []byte {
	alloc := memory.NewGoAllocator()
	fields := make([]arrow.Field, len(names))
	builders := make([]array.Builder, len(names))

	for i, name := range names {
		if len(values[i]) == 0 {
			fields[i] = arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64}
			builders[i] = array.NewInt64Builder(alloc)
			continue
		}
		switch values[i][0].(type) {
		case int64:
			fields[i] = arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64}
			b := array.NewInt64Builder(alloc)
			for _, v := range values[i] {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.(int64))
				}
			}
			builders[i] = b
		case string:
			fields[i] = arrow.Field{Name: name, Type: arrow.BinaryTypes.String}
			b := array.NewStringBuilder(alloc)
			for _, v := range values[i] {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.(string))
				}
			}
			builders[i] = b
		case float64:
			fields[i] = arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Float64}
			b := array.NewFloat64Builder(alloc)
			for _, v := range values[i] {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.(float64))
				}
			}
			builders[i] = b
		default:
			fields[i] = arrow.Field{Name: name, Type: arrow.BinaryTypes.String}
			b := array.NewStringBuilder(alloc)
			builders[i] = b
		}
	}

	schema := arrow.NewSchema(fields, nil)
	cols := make([]arrow.Array, len(builders))
	for i, b := range builders {
		cols[i] = b.NewArray()
	}
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()

	nrows := 0
	if len(values) > 0 {
		nrows = len(values[0])
	}
	rec := array.NewRecord(schema, cols, int64(nrows))
	defer rec.Release()

	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if err := writer.Write(rec); err != nil {
		panic(err)
	}
	writer.Close()
	return buf.Bytes()
}

// --- Tests ---

func TestExecuteQuery_Success(t *testing.T) {
	arrowData := buildArrowIPC(
		[]string{"id", "name"},
		[][]interface{}{{int64(1), int64(2)}, {"alice", "bob"}},
	)
	mock := &mockQueryServiceClient{
		executeQueryFunc: func(_ context.Context, req *connect.Request[queryv1.ExecuteQueryRequest]) (*connect.Response[queryv1.ExecuteQueryResponse], error) {
			assert.Equal(t, "SELECT 1", req.Msg.Sql)
			assert.Equal(t, int32(100), req.Msg.Limit)
			return connect.NewResponse(&queryv1.ExecuteQueryResponse{
				Columns: []*queryv1.ColumnMeta{
					{Name: "id", Type: "INTEGER"},
					{Name: "name", Type: "VARCHAR"},
				},
				ArrowBatch: arrowData,
				TotalRows:  2,
				DurationMs: 5,
			}), nil
		},
	}

	client := newClientWithRPC(mock)
	result, err := client.ExecuteQuery(context.Background(), "SELECT 1", "default", 100)

	require.NoError(t, err)
	assert.Equal(t, 2, result.TotalRows)
	assert.Equal(t, int64(5), result.DurationMs)
	assert.Len(t, result.Columns, 2)
	assert.Len(t, result.Rows, 2)
	assert.Equal(t, int64(1), result.Rows[0]["id"])
	assert.Equal(t, "alice", result.Rows[0]["name"])
}

func TestExecuteQuery_Error(t *testing.T) {
	mock := &mockQueryServiceClient{
		executeQueryFunc: func(_ context.Context, _ *connect.Request[queryv1.ExecuteQueryRequest]) (*connect.Response[queryv1.ExecuteQueryResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))
		},
	}

	client := newClientWithRPC(mock)
	_, err := client.ExecuteQuery(context.Background(), "SELECT 1", "default", 100)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execute query")
}

func TestListTables_Success(t *testing.T) {
	mock := &mockQueryServiceClient{
		listTablesFunc: func(_ context.Context, req *connect.Request[queryv1.ListTablesRequest]) (*connect.Response[queryv1.ListTablesResponse], error) {
			assert.Equal(t, "default", req.Msg.Namespace)
			assert.Equal(t, commonv1.Layer_LAYER_SILVER, req.Msg.Layer)
			return connect.NewResponse(&queryv1.ListTablesResponse{
				Tables: []*queryv1.TableInfo{
					{Namespace: "default", Layer: commonv1.Layer_LAYER_SILVER, Name: "orders", RowCount: 100},
				},
			}), nil
		},
	}

	client := newClientWithRPC(mock)
	tables, err := client.ListTables(context.Background(), "default", "silver")

	require.NoError(t, err)
	assert.Len(t, tables, 1)
	assert.Equal(t, "orders", tables[0].Name)
	assert.Equal(t, "silver", tables[0].Layer)
	assert.Equal(t, int64(100), tables[0].RowCount)
}

func TestGetTable_Success(t *testing.T) {
	mock := &mockQueryServiceClient{
		getSchemaFunc: func(_ context.Context, req *connect.Request[queryv1.GetSchemaRequest]) (*connect.Response[queryv1.GetSchemaResponse], error) {
			assert.Equal(t, commonv1.Layer_LAYER_GOLD, req.Msg.Layer)
			assert.Equal(t, "revenue", req.Msg.TableName)
			return connect.NewResponse(&queryv1.GetSchemaResponse{
				Columns: []*queryv1.ColumnMeta{
					{Name: "amount", Type: "DOUBLE"},
				},
				RowCount:  500,
				SizeBytes: 0,
			}), nil
		},
	}

	client := newClientWithRPC(mock)
	detail, err := client.GetTable(context.Background(), "default", "gold", "revenue")

	require.NoError(t, err)
	assert.Equal(t, "revenue", detail.Name)
	assert.Equal(t, "gold", detail.Layer)
	assert.Equal(t, int64(500), detail.RowCount)
	assert.Len(t, detail.Columns, 1)
	assert.Equal(t, "amount", detail.Columns[0].Name)
}

func TestPreviewTable_Success(t *testing.T) {
	arrowData := buildArrowIPC(
		[]string{"x"},
		[][]interface{}{{int64(1), int64(2), int64(3)}},
	)
	mock := &mockQueryServiceClient{
		previewFunc: func(_ context.Context, req *connect.Request[queryv1.PreviewTableRequest]) (*connect.Response[queryv1.PreviewTableResponse], error) {
			assert.Equal(t, int32(50), req.Msg.Limit)
			return connect.NewResponse(&queryv1.PreviewTableResponse{
				Columns: []*queryv1.ColumnMeta{
					{Name: "x", Type: "INTEGER"},
				},
				ArrowBatch: arrowData,
			}), nil
		},
	}

	client := newClientWithRPC(mock)
	result, err := client.PreviewTable(context.Background(), "default", "silver", "orders", 50)

	require.NoError(t, err)
	assert.Len(t, result.Rows, 3)
}

func TestExecuteQuery_EmptyResult(t *testing.T) {
	mock := &mockQueryServiceClient{
		executeQueryFunc: func(_ context.Context, _ *connect.Request[queryv1.ExecuteQueryRequest]) (*connect.Response[queryv1.ExecuteQueryResponse], error) {
			return connect.NewResponse(&queryv1.ExecuteQueryResponse{
				Columns:    []*queryv1.ColumnMeta{},
				ArrowBatch: nil,
				TotalRows:  0,
			}), nil
		},
	}

	client := newClientWithRPC(mock)
	result, err := client.ExecuteQuery(context.Background(), "SELECT 1 WHERE FALSE", "default", 100)

	require.NoError(t, err)
	assert.Len(t, result.Rows, 0)
	assert.Equal(t, 0, result.TotalRows)
}

func TestArrowToRows_NullValues(t *testing.T) {
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	b := array.NewInt64Builder(alloc)
	b.Append(1)
	b.AppendNull()
	b.Append(3)
	col := b.NewArray()
	defer col.Release()

	rec := array.NewRecord(schema, []arrow.Array{col}, 3)
	defer rec.Release()

	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, writer.Write(rec))
	writer.Close()

	rows, err := arrowutil.IPCToRows(buf.Bytes())
	require.NoError(t, err)
	assert.Len(t, rows, 3)
	assert.Equal(t, int64(1), rows[0]["id"])
	assert.Nil(t, rows[1]["id"])
	assert.Equal(t, int64(3), rows[2]["id"])
}

func TestStringToProtoLayer(t *testing.T) {
	assert.Equal(t, commonv1.Layer_LAYER_BRONZE, stringToProtoLayer("bronze"))
	assert.Equal(t, commonv1.Layer_LAYER_SILVER, stringToProtoLayer("silver"))
	assert.Equal(t, commonv1.Layer_LAYER_GOLD, stringToProtoLayer("gold"))
	assert.Equal(t, commonv1.Layer_LAYER_UNSPECIFIED, stringToProtoLayer(""))
	assert.Equal(t, commonv1.Layer_LAYER_UNSPECIFIED, stringToProtoLayer("unknown"))
}
