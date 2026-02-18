package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextHandler_IncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewContextHandler(base)
	logger := slog.New(handler)

	ctx := ContextWithRequestID(context.Background(), "test-req-123")
	logger.InfoContext(ctx, "test message")

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)
	assert.Equal(t, "test-req-123", entry["request_id"])
	assert.Equal(t, "test message", entry["msg"])
}

func TestContextHandler_NoRequestID_OmitsField(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewContextHandler(base)
	logger := slog.New(handler)

	logger.InfoContext(context.Background(), "no request id")

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)
	assert.Nil(t, entry["request_id"])
	assert.Equal(t, "no request id", entry["msg"])
}

func TestContextHandler_WithAttrs_Preserves(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewContextHandler(base)
	logger := slog.New(handler).With("service", "ratd")

	ctx := ContextWithRequestID(context.Background(), "req-456")
	logger.InfoContext(ctx, "with attrs")

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)
	assert.Equal(t, "req-456", entry["request_id"])
	assert.Equal(t, "ratd", entry["service"])
}

func TestContextHandler_WithGroup_Preserves(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewContextHandler(base)
	logger := slog.New(handler).WithGroup("http")

	ctx := ContextWithRequestID(context.Background(), "req-789")
	logger.InfoContext(ctx, "grouped")

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)
	// When a group is active, record.AddAttrs places request_id inside the group
	httpGroup, ok := entry["http"].(map[string]interface{})
	require.True(t, ok, "expected 'http' group in log entry")
	assert.Equal(t, "req-789", httpGroup["request_id"])
}
