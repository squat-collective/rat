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

// createTestPipeline creates a pipeline and returns it for use as a run's parent.
func createTestPipeline(t *testing.T, store *postgres.PipelineStore, ns, layer, name string) *domain.Pipeline {
	t.Helper()
	p := newTestPipeline(ns, layer, name)
	require.NoError(t, store.CreatePipeline(context.Background(), p))
	return p
}

func TestRunStore_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "orders")

	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    "manual",
	}
	err := rStore.CreateRun(ctx, run)
	require.NoError(t, err)
	assert.NotEmpty(t, run.ID)

	got, err := rStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, domain.RunStatusPending, got.Status)
	assert.Equal(t, "manual", got.Trigger)
}

func TestRunStore_ListFilterByStatus(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "orders")

	run1 := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	run2 := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run1))
	require.NoError(t, rStore.CreateRun(ctx, run2))

	// Move run2 to running
	require.NoError(t, rStore.UpdateRunStatus(ctx, run2.ID.String(), domain.RunStatusRunning, nil, nil, nil))

	runs, err := rStore.ListRuns(ctx, api.RunFilter{Status: "pending"})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, run1.ID, runs[0].ID)
}

func TestRunStore_ListFilterByPipeline(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	p1 := createTestPipeline(t, pStore, "default", "bronze", "orders")
	p2 := createTestPipeline(t, pStore, "default", "silver", "customers")

	r1 := &domain.Run{PipelineID: p1.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	r2 := &domain.Run{PipelineID: p2.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, r1))
	require.NoError(t, rStore.CreateRun(ctx, r2))

	runs, err := rStore.ListRuns(ctx, api.RunFilter{Pipeline: "customers"})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, r2.ID, runs[0].ID)
}

func TestRunStore_UpdateStatus_SetsTimestamps(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "orders")

	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	// Move to running → should set started_at
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusRunning, nil, nil, nil))

	got, err := rStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	assert.NotNil(t, got.StartedAt)
	assert.Nil(t, got.FinishedAt)

	// Move to success → should set finished_at + duration_ms
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusSuccess, nil, nil, nil))

	got, err = rStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	assert.NotNil(t, got.FinishedAt)
	assert.NotNil(t, got.DurationMs)
}

func TestRunStore_GetRunLogs_ReturnsEmpty(t *testing.T) {
	pool := testPool(t)
	rStore := postgres.NewRunStore(pool)

	logs, err := rStore.GetRunLogs(context.Background(), "any-id")
	require.NoError(t, err)
	assert.Empty(t, logs)
}
