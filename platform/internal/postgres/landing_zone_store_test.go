package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLandingZoneStore_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{
		Namespace:   "default",
		Name:        "raw-uploads",
		Description: "test zone",
	}
	err := store.CreateZone(ctx, z)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, z.ID)
	assert.False(t, z.CreatedAt.IsZero())

	got, err := store.GetZone(ctx, "default", "raw-uploads")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, z.ID, got.ID)
	assert.Equal(t, "raw-uploads", got.Name)
	assert.Equal(t, "test zone", got.Description)
	assert.Equal(t, 0, got.FileCount)
	assert.Equal(t, int64(0), got.TotalBytes)
}

func TestLandingZoneStore_CreateDuplicate_ReturnsError(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{Namespace: "default", Name: "uploads"}
	require.NoError(t, store.CreateZone(ctx, z))

	dup := &domain.LandingZone{Namespace: "default", Name: "uploads"}
	err := store.CreateZone(ctx, dup)
	assert.Error(t, err)
}

func TestLandingZoneStore_ListFilterByNamespace(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	nsStore := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	require.NoError(t, nsStore.CreateNamespace(ctx, "analytics", nil))

	require.NoError(t, store.CreateZone(ctx, &domain.LandingZone{Namespace: "default", Name: "zone-a"}))
	require.NoError(t, store.CreateZone(ctx, &domain.LandingZone{Namespace: "analytics", Name: "zone-b"}))

	zones, err := store.ListZones(ctx, api.LandingZoneFilter{Namespace: "analytics"})
	require.NoError(t, err)
	require.Len(t, zones, 1)
	assert.Equal(t, "zone-b", zones[0].Name)
}

func TestLandingZoneStore_Delete(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	require.NoError(t, store.CreateZone(ctx, &domain.LandingZone{Namespace: "default", Name: "to-delete"}))

	err := store.DeleteZone(ctx, "default", "to-delete")
	require.NoError(t, err)

	got, err := store.GetZone(ctx, "default", "to-delete")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLandingZoneStore_GetNotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)

	got, err := store.GetZone(context.Background(), "default", "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLandingZoneStore_FileOperations(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{Namespace: "default", Name: "file-test"}
	require.NoError(t, store.CreateZone(ctx, z))

	f := &domain.LandingFile{
		ZoneID:      z.ID,
		Filename:    "orders.csv",
		S3Path:      "default/landing/file-test/orders.csv",
		SizeBytes:   1234,
		ContentType: "text/csv",
	}
	err := store.CreateFile(ctx, f)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, f.ID)

	// List files
	files, err := store.ListFiles(ctx, z.ID)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "orders.csv", files[0].Filename)
	assert.Equal(t, int64(1234), files[0].SizeBytes)

	// Get file
	got, err := store.GetFile(ctx, f.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "orders.csv", got.Filename)

	// Zone stats should reflect the file
	zone, err := store.GetZone(ctx, "default", "file-test")
	require.NoError(t, err)
	assert.Equal(t, 1, zone.FileCount)
	assert.Equal(t, int64(1234), zone.TotalBytes)

	// Delete file
	err = store.DeleteFile(ctx, f.ID)
	require.NoError(t, err)

	files, err = store.ListFiles(ctx, z.ID)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestLandingZoneStore_GetZoneByID(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{Namespace: "default", Name: "by-id-test"}
	require.NoError(t, store.CreateZone(ctx, z))

	got, err := store.GetZoneByID(ctx, z.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "by-id-test", got.Name)
}

func TestLandingZoneStore_CascadeDelete(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{Namespace: "default", Name: "cascade-test"}
	require.NoError(t, store.CreateZone(ctx, z))

	f := &domain.LandingFile{
		ZoneID:   z.ID,
		Filename: "data.csv",
		S3Path:   "default/landing/cascade-test/data.csv",
	}
	require.NoError(t, store.CreateFile(ctx, f))

	// Delete zone — should cascade delete files
	require.NoError(t, store.DeleteZone(ctx, "default", "cascade-test"))

	got, err := store.GetFile(ctx, f.ID)
	require.NoError(t, err)
	assert.Nil(t, got)
}
