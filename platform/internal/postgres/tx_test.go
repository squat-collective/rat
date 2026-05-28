package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
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

// ---------------------------------------------------------------------------
// InTx + TxRunner — generic transactional helper
// ---------------------------------------------------------------------------

// TestTxRunner_CreateRunAndUpdateTriggerFired_Commits is the success case for
// the "fire a webhook trigger" path: a run is created AND the trigger's
// last_run_id is updated, both inside a single transaction. After commit
// both rows are present and consistent.
func TestTxRunner_CreateRunAndUpdateTriggerFired_Commits(t *testing.T) {
	pool := testPool(t)
	pipelineStore := postgres.NewPipelineStore(pool)
	runStore := postgres.NewRunStore(pool)
	triggerStore := postgres.NewTriggerStore(pool)
	runner := postgres.NewTxRunner(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pipelineStore, "default", "bronze", "tx-fire-ok")
	trigger := createTestTrigger(t, triggerStore, pipeline.ID, domain.TriggerTypeWebhook,
		json.RawMessage(`{"token_hash": "abc"}`))

	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    "trigger:webhook:test",
	}

	err := runner.InTx(ctx, func(t api.TxStores) error {
		if err := t.Runs.CreateRun(ctx, run); err != nil {
			return err
		}
		return t.Triggers.UpdateTriggerFired(ctx, trigger.ID.String(), run.ID)
	})
	require.NoError(t, err)

	// Run exists (via the original non-tx store).
	gotRun, err := runStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	require.NotNil(t, gotRun)
	assert.Equal(t, domain.RunStatusPending, gotRun.Status)

	// Trigger updated.
	gotTrig, err := triggerStore.GetTrigger(ctx, trigger.ID.String())
	require.NoError(t, err)
	require.NotNil(t, gotTrig)
	require.NotNil(t, gotTrig.LastRunID)
	assert.Equal(t, run.ID, *gotTrig.LastRunID)
	assert.NotNil(t, gotTrig.LastTriggeredAt)
}

// TestTxRunner_CreateRunAndUpdateTriggerFired_RollsBack is the failure case:
// the second step errors (we force it by referencing a trigger that does not
// exist via an invalid UUID lookup that returns "no rows"). After rollback,
// NO partial state remains — neither the run row nor any trigger update.
//
// We force the rollback by returning an error from the callback after the
// first DB write. The bug the new InTx fixes is exactly this: previously,
// CreateRun would have committed independently and the failed second step
// would leave an orphan pending run.
func TestTxRunner_CreateRunAndUpdateTriggerFired_RollsBack(t *testing.T) {
	pool := testPool(t)
	pipelineStore := postgres.NewPipelineStore(pool)
	runStore := postgres.NewRunStore(pool)
	runner := postgres.NewTxRunner(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pipelineStore, "default", "bronze", "tx-fire-fail")

	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    "trigger:webhook:test",
	}

	sentinel := errors.New("simulated second-step failure")

	err := runner.InTx(ctx, func(t api.TxStores) error {
		if err := t.Runs.CreateRun(ctx, run); err != nil {
			return err
		}
		// Force a failure AFTER the run was created so we can prove the
		// run row never lands in the DB.
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.NotEqual(t, uuid.Nil, run.ID, "CreateRun should still populate the ID in memory")

	// The hallmark of the fix: the run row must NOT be visible after rollback.
	gotRun, err := runStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	assert.Nil(t, gotRun, "run row should not exist after tx rollback")
}

// TestTxRunner_PanicInsideCallback_RollsBackAndRepanics proves the defer-
// recover discipline in InTx: a panic inside the callback rolls the tx back
// AND re-panics so the original stack still reaches the process-level
// handler (we never silently swallow panics for "atomicity").
func TestTxRunner_PanicInsideCallback_RollsBackAndRepanics(t *testing.T) {
	pool := testPool(t)
	pipelineStore := postgres.NewPipelineStore(pool)
	runStore := postgres.NewRunStore(pool)
	runner := postgres.NewTxRunner(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pipelineStore, "default", "bronze", "tx-fire-panic")

	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    "trigger:webhook:test",
	}

	// We expect a panic; capture it so the test does not fail.
	var got interface{}
	func() {
		defer func() { got = recover() }()
		_ = runner.InTx(ctx, func(t api.TxStores) error {
			_ = t.Runs.CreateRun(ctx, run)
			panic("boom inside tx")
		})
	}()
	require.Equal(t, "boom inside tx", got)

	// And the run row must not be present.
	if run.ID != uuid.Nil {
		gotRun, err := runStore.GetRun(ctx, run.ID.String())
		require.NoError(t, err)
		assert.Nil(t, gotRun, "run row should not exist after panic rollback")
	}
}
