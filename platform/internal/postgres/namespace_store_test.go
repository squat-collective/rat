package postgres_test

import (
	"context"
	"testing"

	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceStore_ListNamespaces_ReturnsDefault(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewNamespaceStore(pool)

	namespaces, err := store.ListNamespaces(context.Background())
	require.NoError(t, err)
	require.Len(t, namespaces, 1)
	assert.Equal(t, "default", namespaces[0].Name)
}

func TestNamespaceStore_CreateAndList(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	err := store.CreateNamespace(ctx, "analytics", nil)
	require.NoError(t, err)

	namespaces, err := store.ListNamespaces(ctx)
	require.NoError(t, err)
	require.Len(t, namespaces, 2)

	names := []string{namespaces[0].Name, namespaces[1].Name}
	assert.Contains(t, names, "default")
	assert.Contains(t, names, "analytics")
}

func TestNamespaceStore_DeleteNamespace(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	err := store.CreateNamespace(ctx, "temporary", nil)
	require.NoError(t, err)

	err = store.DeleteNamespace(ctx, "temporary")
	require.NoError(t, err)

	namespaces, err := store.ListNamespaces(ctx)
	require.NoError(t, err)
	require.Len(t, namespaces, 1)
	assert.Equal(t, "default", namespaces[0].Name)
}
