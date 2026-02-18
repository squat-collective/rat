// Package arrowutil provides shared Arrow IPC deserialization for converting
// Arrow record batches into JSON-serializable row maps. Used by both the
// query client (ratq responses) and the executor (runner preview responses).
package arrowutil

import (
	"bytes"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// IPCToRows converts Arrow IPC bytes to JSON-serializable row maps.
// Returns an empty (non-nil) slice if data is empty.
func IPCToRows(data []byte) ([]map[string]interface{}, error) {
	if len(data) == 0 {
		return []map[string]interface{}{}, nil
	}

	alloc := memory.NewGoAllocator()
	reader, err := ipc.NewReader(bytes.NewReader(data), ipc.WithAllocator(alloc))
	if err != nil {
		return nil, fmt.Errorf("open arrow reader: %w", err)
	}
	defer reader.Release()

	var rows []map[string]interface{}
	for reader.Next() {
		rec := reader.RecordBatch()
		for i := 0; i < int(rec.NumRows()); i++ {
			row := make(map[string]interface{}, int(rec.NumCols()))
			for j := 0; j < int(rec.NumCols()); j++ {
				col := rec.Column(j)
				name := rec.ColumnName(j)
				row[name] = ValueToInterface(col, i)
			}
			rows = append(rows, row)
		}
	}
	if err := reader.Err(); err != nil {
		return nil, fmt.Errorf("read arrow records: %w", err)
	}

	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return rows, nil
}

// ValueToInterface extracts a single value from an Arrow column at the given index.
// Returns nil for null values. Handles all common Arrow types; falls back to
// fmt.Sprintf for unknown types.
func ValueToInterface(col arrow.Array, idx int) interface{} {
	if col.IsNull(idx) {
		return nil
	}
	switch c := col.(type) {
	case *array.Int8:
		return c.Value(idx)
	case *array.Int16:
		return c.Value(idx)
	case *array.Int32:
		return c.Value(idx)
	case *array.Int64:
		return c.Value(idx)
	case *array.Uint8:
		return c.Value(idx)
	case *array.Uint16:
		return c.Value(idx)
	case *array.Uint32:
		return c.Value(idx)
	case *array.Uint64:
		return c.Value(idx)
	case *array.Float32:
		return c.Value(idx)
	case *array.Float64:
		return c.Value(idx)
	case *array.String:
		return c.Value(idx)
	case *array.LargeString:
		return c.Value(idx)
	case *array.Boolean:
		return c.Value(idx)
	case *array.Binary:
		return c.Value(idx)
	case *array.Timestamp:
		v := c.Value(idx)
		dt := c.DataType().(*arrow.TimestampType)
		return v.ToTime(dt.Unit).UTC().Format(time.RFC3339)
	case *array.Date32:
		return c.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return c.Value(idx).ToTime().Format("2006-01-02")
	default:
		// Fallback: use String() to avoid panics from uninitialized ValueStr receivers
		return col.String()
	}
}
