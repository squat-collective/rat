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

func TestScheduleStore_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	sStore := postgres.NewScheduleStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "orders")

	sched := &domain.Schedule{
		PipelineID: pipeline.ID,
		CronExpr:   "0 * * * *",
		Enabled:    true,
	}
	err := sStore.CreateSchedule(ctx, sched)
	require.NoError(t, err)
	assert.NotEmpty(t, sched.ID)

	got, err := sStore.GetSchedule(ctx, sched.ID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "0 * * * *", got.CronExpr)
	assert.True(t, got.Enabled)
}

func TestScheduleStore_ListSchedules(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	sStore := postgres.NewScheduleStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "orders")

	s1 := &domain.Schedule{PipelineID: pipeline.ID, CronExpr: "0 * * * *", Enabled: true}
	s2 := &domain.Schedule{PipelineID: pipeline.ID, CronExpr: "*/5 * * * *", Enabled: false}
	require.NoError(t, sStore.CreateSchedule(ctx, s1))
	require.NoError(t, sStore.CreateSchedule(ctx, s2))

	schedules, err := sStore.ListSchedules(ctx)
	require.NoError(t, err)
	assert.Len(t, schedules, 2)
}

func TestScheduleStore_UpdatePartial(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	sStore := postgres.NewScheduleStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "orders")

	sched := &domain.Schedule{PipelineID: pipeline.ID, CronExpr: "0 * * * *", Enabled: true}
	require.NoError(t, sStore.CreateSchedule(ctx, sched))

	// Update only cron, enabled stays true
	newCron := "*/15 * * * *"
	updated, err := sStore.UpdateSchedule(ctx, sched.ID.String(), api.UpdateScheduleRequest{
		Cron: &newCron,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "*/15 * * * *", updated.CronExpr)
	assert.True(t, updated.Enabled) // unchanged
}

func TestScheduleStore_Delete(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	sStore := postgres.NewScheduleStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "orders")

	sched := &domain.Schedule{PipelineID: pipeline.ID, CronExpr: "0 * * * *", Enabled: true}
	require.NoError(t, sStore.CreateSchedule(ctx, sched))

	err := sStore.DeleteSchedule(ctx, sched.ID.String())
	require.NoError(t, err)

	got, err := sStore.GetSchedule(ctx, sched.ID.String())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestScheduleStore_GetNotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	sStore := postgres.NewScheduleStore(pool)

	got, err := sStore.GetSchedule(context.Background(), "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.Nil(t, got)
}
