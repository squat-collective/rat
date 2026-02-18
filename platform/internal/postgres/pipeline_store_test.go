package postgres_test

import (
	"context"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPipeline(namespace, layer, name string) *domain.Pipeline {
	return &domain.Pipeline{
		Namespace:   namespace,
		Layer:       domain.Layer(layer),
		Name:        name,
		Type:        "sql",
		S3Path:      namespace + "/pipelines/" + layer + "/" + name + "/",
		Description: "test pipeline",
	}
}

func TestPipelineStore_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "orders")
	err := store.CreatePipeline(ctx, p)
	require.NoError(t, err)
	assert.NotEmpty(t, p.ID)
	assert.False(t, p.CreatedAt.IsZero())

	got, err := store.GetPipeline(ctx, "default", "bronze", "orders")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, p.ID, got.ID)
	assert.Equal(t, "orders", got.Name)
	assert.Equal(t, domain.LayerBronze, got.Layer)
	assert.Equal(t, "test pipeline", got.Description)
}

func TestPipelineStore_CreateDuplicate_ReturnsError(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "orders")
	err := store.CreatePipeline(ctx, p)
	require.NoError(t, err)

	dup := newTestPipeline("default", "bronze", "orders")
	err = store.CreatePipeline(ctx, dup)
	assert.Error(t, err)
}

func TestPipelineStore_ListFilterByNamespace(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	nsStore := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	err := nsStore.CreateNamespace(ctx, "analytics", nil)
	require.NoError(t, err)

	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", "orders")))
	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("analytics", "bronze", "events")))

	pipelines, err := store.ListPipelines(ctx, api.PipelineFilter{Namespace: "analytics"})
	require.NoError(t, err)
	require.Len(t, pipelines, 1)
	assert.Equal(t, "events", pipelines[0].Name)
}

func TestPipelineStore_ListFilterByLayer(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", "raw_orders")))
	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "silver", "clean_orders")))

	pipelines, err := store.ListPipelines(ctx, api.PipelineFilter{Layer: "silver"})
	require.NoError(t, err)
	require.Len(t, pipelines, 1)
	assert.Equal(t, "clean_orders", pipelines[0].Name)
}

func TestPipelineStore_UpdatePartial(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", "orders")))

	desc := "updated description"
	updated, err := store.UpdatePipeline(ctx, "default", "bronze", "orders", api.UpdatePipelineRequest{
		Description: &desc,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "updated description", updated.Description)
	assert.Equal(t, "sql", updated.Type) // unchanged
}

func TestPipelineStore_SoftDelete(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", "orders")))

	err := store.DeletePipeline(ctx, "default", "bronze", "orders")
	require.NoError(t, err)

	// Should not appear in Get or List
	got, err := store.GetPipeline(ctx, "default", "bronze", "orders")
	require.NoError(t, err)
	assert.Nil(t, got)

	pipelines, err := store.ListPipelines(ctx, api.PipelineFilter{})
	require.NoError(t, err)
	assert.Empty(t, pipelines)
}

func TestPipelineStore_GetNotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)

	got, err := store.GetPipeline(context.Background(), "default", "bronze", "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}
