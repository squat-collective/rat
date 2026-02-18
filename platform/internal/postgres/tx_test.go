package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishPipelineTx_AtomicPublishVersionAndPrune(t *testing.T) {
	pool := testPool(t)
	pipelineStore := postgres.NewPipelineStore(pool)
	versionStore := postgres.NewVersionStore(pool)
	publisher := postgres.NewPipelinePublisher(pool)
	ctx := context.Background()

	// Create a pipeline
	p := newTestPipeline("default", "bronze", "orders")
	require.NoError(t, pipelineStore.CreatePipeline(ctx, p))
	require.NotEmpty(t, p.ID)

	versions := map[string]string{"pipeline.sql": "v1-abc"}
	pv := &domain.PipelineVersion{
		PipelineID:        p.ID,
		VersionNumber:     1,
		Message:           "Initial publish",
		PublishedVersions: versions,
	}

	err := publisher.PublishPipelineTx(ctx, "default", "bronze", "orders", versions, pv, 50)
	require.NoError(t, err)

	// Verify pipeline was published
	got, err := pipelineStore.GetPipeline(ctx, "default", "bronze", "orders")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotNil(t, got.PublishedAt)
	assert.Equal(t, "v1-abc", got.PublishedVersions["pipeline.sql"])
	assert.False(t, got.DraftDirty)

	// Verify version record was created
	v, err := versionStore.GetVersion(ctx, p.ID, 1)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, "Initial publish", v.Message)
	assert.Equal(t, "v1-abc", v.PublishedVersions["pipeline.sql"])
}

func TestPublishPipelineTx_PrunesOldVersions(t *testing.T) {
	pool := testPool(t)
	pipelineStore := postgres.NewPipelineStore(pool)
	versionStore := postgres.NewVersionStore(pool)
	publisher := postgres.NewPipelinePublisher(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "prunable")
	require.NoError(t, pipelineStore.CreatePipeline(ctx, p))

	// Create 3 versions, then publish a 4th with keepCount=3
	for i := 1; i <= 3; i++ {
		pv := &domain.PipelineVersion{
			PipelineID:        p.ID,
			VersionNumber:     i,
			Message:           fmt.Sprintf("v%d", i),
			PublishedVersions: map[string]string{"f.sql": "vid"},
		}
		require.NoError(t, versionStore.CreateVersion(ctx, pv))
	}

	pv := &domain.PipelineVersion{
		PipelineID:        p.ID,
		VersionNumber:     4,
		Message:           "v4",
		PublishedVersions: map[string]string{"f.sql": "vid4"},
	}
	err := publisher.PublishPipelineTx(ctx, "default", "bronze", "prunable", map[string]string{"f.sql": "vid4"}, pv, 3)
	require.NoError(t, err)

	// v1 should be pruned; v2, v3, v4 remain
	all, err := versionStore.ListVersions(ctx, p.ID)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	v1, err := versionStore.GetVersion(ctx, p.ID, 1)
	require.NoError(t, err)
	assert.Nil(t, v1, "v1 should have been pruned")
}

func TestRollbackPipelineTx_AtomicRollback(t *testing.T) {
	pool := testPool(t)
	pipelineStore := postgres.NewPipelineStore(pool)
	versionStore := postgres.NewVersionStore(pool)
	publisher := postgres.NewPipelinePublisher(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "silver", "rollbackable")
	require.NoError(t, pipelineStore.CreatePipeline(ctx, p))

	// Create v1 and v2
	v1Versions := map[string]string{"pipeline.sql": "old-vid"}
	pv1 := &domain.PipelineVersion{
		PipelineID:        p.ID,
		VersionNumber:     1,
		Message:           "v1",
		PublishedVersions: v1Versions,
	}
	require.NoError(t, versionStore.CreateVersion(ctx, pv1))

	v2Versions := map[string]string{"pipeline.sql": "new-vid"}
	pv2 := &domain.PipelineVersion{
		PipelineID:        p.ID,
		VersionNumber:     2,
		Message:           "v2",
		PublishedVersions: v2Versions,
	}
	require.NoError(t, versionStore.CreateVersion(ctx, pv2))

	// Publish v2 so the pipeline has published_versions set
	require.NoError(t, pipelineStore.PublishPipeline(ctx, "default", "silver", "rollbackable", v2Versions))

	// Rollback to v1 — creates v3 with v1's snapshot
	rollbackPV := &domain.PipelineVersion{
		PipelineID:        p.ID,
		VersionNumber:     3,
		Message:           "Rollback to v1",
		PublishedVersions: v1Versions,
	}
	err := publisher.RollbackPipelineTx(ctx, "default", "silver", "rollbackable", v1Versions, rollbackPV, 50)
	require.NoError(t, err)

	// Verify v3 was created with v1's snapshot
	v3, err := versionStore.GetVersion(ctx, p.ID, 3)
	require.NoError(t, err)
	require.NotNil(t, v3)
	assert.Equal(t, "Rollback to v1", v3.Message)
	assert.Equal(t, "old-vid", v3.PublishedVersions["pipeline.sql"])

	// Verify pipeline now points to v1's snapshot
	got, err := pipelineStore.GetPipeline(ctx, "default", "silver", "rollbackable")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "old-vid", got.PublishedVersions["pipeline.sql"])
}
