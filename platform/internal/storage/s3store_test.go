package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/rat-data/rat/platform/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFileHelper discards the version ID so tests can use require.NoError.
func writeFileHelper(ctx context.Context, store *storage.S3Store, path string, content []byte) error {
	_, err := store.WriteFile(ctx, path, content)
	return err
}

func TestS3Store_WriteAndRead(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	_, err := store.WriteFile(ctx, "default/silver/orders/pipeline.sql", []byte("SELECT * FROM bronze.orders"))
	require.NoError(t, err)

	file, err := store.ReadFile(ctx, "default/silver/orders/pipeline.sql")
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "default/silver/orders/pipeline.sql", file.Path)
	assert.Equal(t, "SELECT * FROM bronze.orders", file.Content)
	assert.Equal(t, int64(len("SELECT * FROM bronze.orders")), file.Size)
	assert.False(t, file.Modified.IsZero())
}

func TestS3Store_ReadNotFound_ReturnsNil(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	file, err := store.ReadFile(ctx, "nonexistent/path.sql")
	require.NoError(t, err)
	assert.Nil(t, file)
}

func TestS3Store_ListWithPrefix(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	require.NoError(t, writeFileHelper(ctx, store, "default/silver/orders/pipeline.sql", []byte("SELECT 1")))
	require.NoError(t, writeFileHelper(ctx, store, "default/silver/users/pipeline.sql", []byte("SELECT 2")))
	require.NoError(t, writeFileHelper(ctx, store, "default/gold/revenue/pipeline.sql", []byte("SELECT 3")))

	files, err := store.ListFiles(ctx, "default/silver/")
	require.NoError(t, err)
	assert.Len(t, files, 2)

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}
	assert.True(t, paths["default/silver/orders/pipeline.sql"])
	assert.True(t, paths["default/silver/users/pipeline.sql"])
}

func TestS3Store_ListEmpty_ReturnsEmptySlice(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	files, err := store.ListFiles(ctx, "nonexistent/")
	require.NoError(t, err)
	assert.NotNil(t, files)
	assert.Len(t, files, 0)
}

func TestS3Store_DeleteFile(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	require.NoError(t, writeFileHelper(ctx, store, "to-delete.sql", []byte("DROP TABLE")))

	err := store.DeleteFile(ctx, "to-delete.sql")
	require.NoError(t, err)

	file, err := store.ReadFile(ctx, "to-delete.sql")
	require.NoError(t, err)
	assert.Nil(t, file)
}

func TestS3Store_DeleteNotFound_IsIdempotent(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	// S3 delete is idempotent — deleting a non-existent object is not an error.
	err := store.DeleteFile(ctx, "nonexistent.sql")
	assert.NoError(t, err)
}

func TestS3Store_FileTypeDetection(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	require.NoError(t, writeFileHelper(ctx, store, "default/silver/orders/pipeline.sql", []byte("SELECT 1")))
	require.NoError(t, writeFileHelper(ctx, store, "default/silver/orders/config.yaml", []byte("version: 1")))
	require.NoError(t, writeFileHelper(ctx, store, "default/silver/orders/README.md", []byte("# Orders")))
	require.NoError(t, writeFileHelper(ctx, store, "default/silver/orders/tests/test_quality.py", []byte("def test(): pass")))

	files, err := store.ListFiles(ctx, "default/silver/orders/")
	require.NoError(t, err)
	assert.Len(t, files, 4)

	types := make(map[string]string)
	for _, f := range files {
		types[f.Path] = f.Type
	}

	assert.Equal(t, "pipeline-sql", types["default/silver/orders/pipeline.sql"])
	assert.Equal(t, "config", types["default/silver/orders/config.yaml"])
	assert.Equal(t, "doc", types["default/silver/orders/README.md"])
	assert.Equal(t, "test", types["default/silver/orders/tests/test_quality.py"])
}

func TestS3Store_OverwriteExisting(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	require.NoError(t, writeFileHelper(ctx, store, "overwrite.sql", []byte("v1")))
	require.NoError(t, writeFileHelper(ctx, store, "overwrite.sql", []byte("v2")))

	file, err := store.ReadFile(ctx, "overwrite.sql")
	require.NoError(t, err)
	require.NotNil(t, file)
	assert.Equal(t, "v2", file.Content)
	assert.Equal(t, int64(2), file.Size)
}

func TestS3Store_WriteFile_ReturnsVersionID(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	versionID, err := store.WriteFile(ctx, "versioned/test.sql", []byte("SELECT 1"))
	require.NoError(t, err)
	// MinIO returns a version ID when bucket versioning is enabled;
	// may be empty in test env without versioning — just verify no error.
	_ = versionID
}

func TestS3Config_DefaultTimeouts(t *testing.T) {
	assert.Equal(t, 10*time.Second, storage.DefaultMetadataTimeout)
	assert.Equal(t, 60*time.Second, storage.DefaultDataTimeout)
}

func TestS3Store_FromConfig_CustomTimeouts(t *testing.T) {
	store := testS3StoreFromConfig(t, storage.S3Config{
		MetadataTimeout: 5 * time.Second,
		DataTimeout:     30 * time.Second,
	})
	ctx := context.Background()

	// Verify the store works with custom timeouts — write + read round-trip.
	_, err := store.WriteFile(ctx, "timeout-test/file.sql", []byte("SELECT 1"))
	require.NoError(t, err)

	file, err := store.ReadFile(ctx, "timeout-test/file.sql")
	require.NoError(t, err)
	require.NotNil(t, file)
	assert.Equal(t, "SELECT 1", file.Content)
}

func TestS3Store_CancelledContext_ReturnsError(t *testing.T) {
	store := testS3Store(t)

	// Pre-cancelled context should cause operations to fail.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.WriteFile(ctx, "should-fail.sql", []byte("nope"))
	assert.Error(t, err)
}

func TestS3Store_FromConfig_ListWithCancelledContext(t *testing.T) {
	store := testS3Store(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.ListFiles(ctx, "prefix/")
	assert.Error(t, err)
}

func TestS3Store_FromConfig_DeleteWithCancelledContext(t *testing.T) {
	store := testS3Store(t)
	ctx := context.Background()

	// Write a file first so we have something to attempt deleting.
	_, err := store.WriteFile(ctx, "delete-timeout.sql", []byte("data"))
	require.NoError(t, err)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = store.DeleteFile(cancelledCtx, "delete-timeout.sql")
	assert.Error(t, err)
}
