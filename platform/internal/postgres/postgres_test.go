package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanExtraTables truncates tables not covered by the shared cleanTables helper.
// Called at the start of tests that touch audit_log, table_metadata, or platform_settings.
func cleanExtraTables(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	ctx := context.Background()
	for _, table := range tables {
		if _, err := pool.Exec(ctx, "TRUNCATE "+table+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

// ---------------------------------------------------------------------------
// VersionStore tests
// ---------------------------------------------------------------------------

func TestVersionStore_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	vStore := postgres.NewVersionStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "orders")
	require.NoError(t, pStore.CreatePipeline(ctx, p))

	v := &domain.PipelineVersion{
		PipelineID:        p.ID,
		VersionNumber:     1,
		Message:           "Initial version",
		PublishedVersions: map[string]string{"pipeline.sql": "vid-abc"},
	}
	err := vStore.CreateVersion(ctx, v)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, v.ID)
	assert.False(t, v.CreatedAt.IsZero())

	got, err := vStore.GetVersion(ctx, p.ID, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, v.ID, got.ID)
	assert.Equal(t, 1, got.VersionNumber)
	assert.Equal(t, "Initial version", got.Message)
	assert.Equal(t, "vid-abc", got.PublishedVersions["pipeline.sql"])
}

func TestVersionStore_GetNotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	vStore := postgres.NewVersionStore(pool)

	got, err := vStore.GetVersion(context.Background(), uuid.New(), 999)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestVersionStore_ListVersions_OrderedDescending(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	vStore := postgres.NewVersionStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "list-versions")
	require.NoError(t, pStore.CreatePipeline(ctx, p))

	for i := 1; i <= 3; i++ {
		require.NoError(t, vStore.CreateVersion(ctx, &domain.PipelineVersion{
			PipelineID:        p.ID,
			VersionNumber:     i,
			Message:           fmt.Sprintf("v%d", i),
			PublishedVersions: map[string]string{"f.sql": fmt.Sprintf("vid-%d", i)},
		}))
	}

	versions, err := vStore.ListVersions(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)

	// Should be descending by version_number
	assert.Equal(t, 3, versions[0].VersionNumber)
	assert.Equal(t, 2, versions[1].VersionNumber)
	assert.Equal(t, 1, versions[2].VersionNumber)
}

func TestVersionStore_LatestVersionNumber_NoVersions(t *testing.T) {
	pool := testPool(t)
	vStore := postgres.NewVersionStore(pool)

	n, err := vStore.LatestVersionNumber(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestVersionStore_LatestVersionNumber_ReturnsMax(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	vStore := postgres.NewVersionStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "silver", "latest-num")
	require.NoError(t, pStore.CreatePipeline(ctx, p))

	for i := 1; i <= 5; i++ {
		require.NoError(t, vStore.CreateVersion(ctx, &domain.PipelineVersion{
			PipelineID:        p.ID,
			VersionNumber:     i,
			Message:           fmt.Sprintf("v%d", i),
			PublishedVersions: map[string]string{"f.sql": "vid"},
		}))
	}

	n, err := vStore.LatestVersionNumber(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
}

func TestVersionStore_PruneVersions_KeepsRecent(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	vStore := postgres.NewVersionStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "prune-test")
	require.NoError(t, pStore.CreatePipeline(ctx, p))

	for i := 1; i <= 5; i++ {
		require.NoError(t, vStore.CreateVersion(ctx, &domain.PipelineVersion{
			PipelineID:        p.ID,
			VersionNumber:     i,
			Message:           fmt.Sprintf("v%d", i),
			PublishedVersions: map[string]string{"f.sql": "vid"},
		}))
	}

	err := vStore.PruneVersions(ctx, p.ID, 3)
	require.NoError(t, err)

	versions, err := vStore.ListVersions(ctx, p.ID)
	require.NoError(t, err)
	assert.Len(t, versions, 3)

	// v1 and v2 should be pruned; v3, v4, v5 remain
	v1, err := vStore.GetVersion(ctx, p.ID, 1)
	require.NoError(t, err)
	assert.Nil(t, v1, "v1 should have been pruned")

	v5, err := vStore.GetVersion(ctx, p.ID, 5)
	require.NoError(t, err)
	assert.NotNil(t, v5, "v5 should remain after pruning")
}

// ---------------------------------------------------------------------------
// TableMetadataStore tests
// ---------------------------------------------------------------------------

func TestTableMetadataStore_UpsertAndGet(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "table_metadata")
	store := postgres.NewTableMetadataStore(pool)
	ctx := context.Background()

	m := &domain.TableMetadata{
		Namespace:          "default",
		Layer:              "bronze",
		Name:               "orders",
		Description:        "Raw orders table",
		ColumnDescriptions: map[string]string{"id": "Primary key", "amount": "Order total"},
	}
	err := store.Upsert(ctx, m)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, m.ID)
	assert.False(t, m.CreatedAt.IsZero())

	got, err := store.Get(ctx, "default", "bronze", "orders")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, "Raw orders table", got.Description)
	assert.Equal(t, "Primary key", got.ColumnDescriptions["id"])
	assert.Equal(t, "Order total", got.ColumnDescriptions["amount"])
}

func TestTableMetadataStore_UpsertUpdatesExisting(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "table_metadata")
	store := postgres.NewTableMetadataStore(pool)
	ctx := context.Background()

	m := &domain.TableMetadata{
		Namespace:          "default",
		Layer:              "silver",
		Name:               "customers",
		Description:        "Original description",
		ColumnDescriptions: map[string]string{"id": "Customer ID"},
	}
	require.NoError(t, store.Upsert(ctx, m))
	origID := m.ID

	// Upsert again with updated description
	m2 := &domain.TableMetadata{
		Namespace:          "default",
		Layer:              "silver",
		Name:               "customers",
		Description:        "Updated description",
		ColumnDescriptions: map[string]string{"id": "Customer ID", "email": "Email address"},
	}
	require.NoError(t, store.Upsert(ctx, m2))

	// Same ID, updated fields
	assert.Equal(t, origID, m2.ID)

	got, err := store.Get(ctx, "default", "silver", "customers")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated description", got.Description)
	assert.Equal(t, "Email address", got.ColumnDescriptions["email"])
}

func TestTableMetadataStore_GetNotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "table_metadata")
	store := postgres.NewTableMetadataStore(pool)

	got, err := store.Get(context.Background(), "default", "bronze", "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTableMetadataStore_ListAll(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "table_metadata")
	store := postgres.NewTableMetadataStore(pool)
	ctx := context.Background()

	m1 := &domain.TableMetadata{
		Namespace:          "default",
		Layer:              "bronze",
		Name:               "users",
		Description:        "Users table",
		ColumnDescriptions: map[string]string{},
	}
	m2 := &domain.TableMetadata{
		Namespace:          "default",
		Layer:              "silver",
		Name:               "orders",
		Description:        "Orders table",
		ColumnDescriptions: map[string]string{},
	}
	require.NoError(t, store.Upsert(ctx, m1))
	require.NoError(t, store.Upsert(ctx, m2))

	all, err := store.ListAll(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)
}

func TestTableMetadataStore_UpsertWithOwner(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "table_metadata")
	store := postgres.NewTableMetadataStore(pool)
	ctx := context.Background()

	owner := "data-team"
	m := &domain.TableMetadata{
		Namespace:          "default",
		Layer:              "gold",
		Name:               "revenue",
		Description:        "Revenue aggregate",
		Owner:              &owner,
		ColumnDescriptions: map[string]string{},
	}
	require.NoError(t, store.Upsert(ctx, m))

	got, err := store.Get(ctx, "default", "gold", "revenue")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Owner)
	assert.Equal(t, "data-team", *got.Owner)
}

// ---------------------------------------------------------------------------
// AuditStore tests
// ---------------------------------------------------------------------------

func TestAuditStore_LogAndList(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "audit_log")
	store := postgres.NewAuditStore(pool)
	ctx := context.Background()

	err := store.Log(ctx, "user-1", "create", "pipeline/default/bronze/orders", "created new pipeline", "127.0.0.1")
	require.NoError(t, err)

	err = store.Log(ctx, "user-1", "update", "pipeline/default/bronze/orders", "updated description", "127.0.0.2")
	require.NoError(t, err)

	entries, err := store.List(ctx, 10, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(entries), 2)

	// Most recent first
	assert.Equal(t, "update", entries[0].Action)
	assert.Equal(t, "create", entries[1].Action)
}

func TestAuditStore_ListEmpty(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "audit_log")
	store := postgres.NewAuditStore(pool)

	entries, err := store.List(context.Background(), 10, 0)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAuditStore_ListWithPagination(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "audit_log")
	store := postgres.NewAuditStore(pool)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Log(ctx, "user-1", "action", "resource", fmt.Sprintf("entry-%d", i), ""))
	}

	// First page
	page1, err := store.List(ctx, 2, 0)
	require.NoError(t, err)
	assert.Len(t, page1, 2)

	// Second page
	page2, err := store.List(ctx, 2, 2)
	require.NoError(t, err)
	assert.Len(t, page2, 2)

	// Pages should not overlap
	assert.NotEqual(t, page1[0].ID, page2[0].ID)
}

func TestAuditStore_DeleteOlderThan(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "audit_log")
	store := postgres.NewAuditStore(pool)
	ctx := context.Background()

	require.NoError(t, store.Log(ctx, "user-1", "action", "resource", "old entry", ""))

	// Delete entries older than 1 second in the future (should delete everything)
	deleted, err := store.DeleteOlderThan(ctx, time.Now().Add(1*time.Second))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, 1)

	entries, err := store.List(ctx, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAuditStore_DeleteOlderThan_KeepsRecent(t *testing.T) {
	pool := testPool(t)
	cleanExtraTables(t, pool, "audit_log")
	store := postgres.NewAuditStore(pool)
	ctx := context.Background()

	require.NoError(t, store.Log(ctx, "user-1", "action", "resource", "recent entry", ""))

	// Delete entries older than 1 hour ago (should delete nothing)
	deleted, err := store.DeleteOlderThan(ctx, time.Now().Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)

	entries, err := store.List(ctx, 10, 0)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

// ---------------------------------------------------------------------------
// SettingsStore tests
// ---------------------------------------------------------------------------

func TestSettingsStore_PutAndGetSetting(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewSettingsStore(pool)
	ctx := context.Background()

	value := json.RawMessage(`{"enabled": true, "interval": 60}`)
	err := store.PutSetting(ctx, "test_setting", value)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM platform_settings WHERE key = 'test_setting'")
	})

	got, err := store.GetSetting(ctx, "test_setting")
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(got, &parsed))
	assert.Equal(t, true, parsed["enabled"])
	assert.Equal(t, float64(60), parsed["interval"])
}

func TestSettingsStore_PutSetting_Upserts(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewSettingsStore(pool)
	ctx := context.Background()

	require.NoError(t, store.PutSetting(ctx, "upsert_key", json.RawMessage(`{"v": 1}`)))
	require.NoError(t, store.PutSetting(ctx, "upsert_key", json.RawMessage(`{"v": 2}`)))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM platform_settings WHERE key = 'upsert_key'")
	})

	got, err := store.GetSetting(ctx, "upsert_key")
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(got, &parsed))
	assert.Equal(t, float64(2), parsed["v"])
}

func TestSettingsStore_GetSetting_NotFound_ReturnsError(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewSettingsStore(pool)

	_, err := store.GetSetting(context.Background(), "nonexistent_key_xyz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSettingsStore_GetReaperStatus(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewSettingsStore(pool)
	ctx := context.Background()

	// Reset reaper_status to migration defaults so the test is deterministic.
	_, err := pool.Exec(ctx,
		`UPDATE reaper_status SET last_run_at = NULL, runs_pruned = 0, logs_pruned = 0,
		 quality_pruned = 0, pipelines_purged = 0, runs_failed = 0,
		 branches_cleaned = 0, lz_files_cleaned = 0, audit_pruned = 0, updated_at = NOW()
		 WHERE id = 1`)
	require.NoError(t, err)

	status, err := store.GetReaperStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 0, status.RunsPruned)
	assert.Equal(t, 0, status.LogsPruned)
}

func TestSettingsStore_UpdateReaperStatus(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewSettingsStore(pool)
	ctx := context.Background()

	status := &domain.ReaperStatus{
		RunsPruned:      10,
		LogsPruned:      5,
		QualityPruned:   3,
		PipelinesPurged: 1,
		RunsFailed:      2,
		BranchesCleaned: 4,
		LZFilesCleaned:  7,
		AuditPruned:     15,
	}
	err := store.UpdateReaperStatus(ctx, status)
	require.NoError(t, err)

	got, err := store.GetReaperStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10, got.RunsPruned)
	assert.Equal(t, 5, got.LogsPruned)
	assert.Equal(t, 3, got.QualityPruned)
	assert.Equal(t, 1, got.PipelinesPurged)
	assert.Equal(t, 2, got.RunsFailed)
	assert.Equal(t, 4, got.BranchesCleaned)
	assert.Equal(t, 7, got.LZFilesCleaned)
	assert.Equal(t, 15, got.AuditPruned)
	assert.NotNil(t, got.LastRunAt) // set by NOW() in the UPDATE
}

func TestSettingsStore_RetentionSetting_SeededByMigration(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewSettingsStore(pool)
	ctx := context.Background()

	raw, err := store.GetSetting(ctx, "retention")
	require.NoError(t, err)

	var config domain.RetentionConfig
	require.NoError(t, json.Unmarshal(raw, &config))
	assert.Equal(t, 100, config.RunsMaxPerPipeline)
	assert.Equal(t, 90, config.RunsMaxAgeDays)
}

// ---------------------------------------------------------------------------
// TriggerStore tests
// ---------------------------------------------------------------------------

func createTestTrigger(t *testing.T, store *postgres.TriggerStore, pipelineID uuid.UUID, triggerType domain.TriggerType, config json.RawMessage) *domain.PipelineTrigger {
	t.Helper()
	trigger := &domain.PipelineTrigger{
		PipelineID:      pipelineID,
		Type:            triggerType,
		Config:          config,
		Enabled:         true,
		CooldownSeconds: 60,
	}
	require.NoError(t, store.CreateTrigger(context.Background(), trigger))
	return trigger
}

func TestTriggerStore_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "trigger-test")

	config := json.RawMessage(`{"namespace": "default", "zone_name": "uploads"}`)
	trigger := createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeLandingZoneUpload, config)

	assert.NotEqual(t, uuid.Nil, trigger.ID)
	assert.False(t, trigger.CreatedAt.IsZero())

	got, err := tStore.GetTrigger(ctx, trigger.ID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, trigger.ID, got.ID)
	assert.Equal(t, domain.TriggerTypeLandingZoneUpload, got.Type)
	assert.True(t, got.Enabled)
	assert.Equal(t, 60, got.CooldownSeconds)
}

func TestTriggerStore_GetNotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	tStore := postgres.NewTriggerStore(pool)

	got, err := tStore.GetTrigger(context.Background(), uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTriggerStore_GetInvalidUUID_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	tStore := postgres.NewTriggerStore(pool)

	got, err := tStore.GetTrigger(context.Background(), "not-a-uuid")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTriggerStore_ListTriggers_ByPipeline(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	p1 := createTestPipeline(t, pStore, "default", "bronze", "t-list-1")
	p2 := createTestPipeline(t, pStore, "default", "silver", "t-list-2")

	createTestTrigger(t, tStore, p1.ID, domain.TriggerTypeCron, json.RawMessage(`{"cron": "0 * * * *"}`))
	createTestTrigger(t, tStore, p1.ID, domain.TriggerTypeWebhook, json.RawMessage(`{"token_hash": "abc123"}`))
	createTestTrigger(t, tStore, p2.ID, domain.TriggerTypeCron, json.RawMessage(`{"cron": "*/5 * * * *"}`))

	// Only p1's triggers
	triggers, err := tStore.ListTriggers(ctx, p1.ID)
	require.NoError(t, err)
	assert.Len(t, triggers, 2)

	// Only p2's triggers
	triggers, err = tStore.ListTriggers(ctx, p2.ID)
	require.NoError(t, err)
	assert.Len(t, triggers, 1)
}

func TestTriggerStore_UpdateTrigger_PartialUpdate(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-update")
	trigger := createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeCron, json.RawMessage(`{"cron": "0 * * * *"}`))

	// Disable the trigger without changing config
	disabled := false
	updated, err := tStore.UpdateTrigger(ctx, trigger.ID.String(), api.UpdateTriggerRequest{
		Enabled: &disabled,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.Enabled)
	assert.Equal(t, 60, updated.CooldownSeconds) // unchanged
}

func TestTriggerStore_UpdateTrigger_CooldownSeconds(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-cooldown")
	trigger := createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeCron, json.RawMessage(`{"cron": "0 * * * *"}`))

	newCooldown := 300
	updated, err := tStore.UpdateTrigger(ctx, trigger.ID.String(), api.UpdateTriggerRequest{
		CooldownSeconds: &newCooldown,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, 300, updated.CooldownSeconds)
	assert.True(t, updated.Enabled) // unchanged
}

func TestTriggerStore_UpdateTrigger_NotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	tStore := postgres.NewTriggerStore(pool)

	disabled := false
	updated, err := tStore.UpdateTrigger(context.Background(), uuid.New().String(), api.UpdateTriggerRequest{
		Enabled: &disabled,
	})
	require.NoError(t, err)
	assert.Nil(t, updated)
}

func TestTriggerStore_DeleteTrigger(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-delete")
	trigger := createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeCron, json.RawMessage(`{"cron": "0 * * * *"}`))

	err := tStore.DeleteTrigger(ctx, trigger.ID.String())
	require.NoError(t, err)

	got, err := tStore.GetTrigger(ctx, trigger.ID.String())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTriggerStore_FindTriggersByType(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-find-type")
	createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeCron, json.RawMessage(`{"cron": "0 * * * *"}`))
	createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeWebhook, json.RawMessage(`{"token_hash": "abc"}`))

	crons, err := tStore.FindTriggersByType(ctx, string(domain.TriggerTypeCron))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(crons), 1)
	for _, tr := range crons {
		assert.Equal(t, domain.TriggerTypeCron, tr.Type)
	}
}

func TestTriggerStore_FindTriggersByLandingZone(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-find-lz")
	config := json.RawMessage(`{"namespace": "default", "zone_name": "raw-uploads"}`)
	createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeLandingZoneUpload, config)

	triggers, err := tStore.FindTriggersByLandingZone(ctx, "default", "raw-uploads")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(triggers), 1)
	assert.Equal(t, domain.TriggerTypeLandingZoneUpload, triggers[0].Type)
}

func TestTriggerStore_FindTriggersByLandingZone_NoMatch(t *testing.T) {
	pool := testPool(t)
	tStore := postgres.NewTriggerStore(pool)

	triggers, err := tStore.FindTriggersByLandingZone(context.Background(), "nonexistent", "zone")
	require.NoError(t, err)
	assert.Empty(t, triggers)
}

func TestTriggerStore_FindTriggersByPipelineSuccess(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "silver", "t-find-ps")
	config := json.RawMessage(`{"namespace": "default", "layer": "bronze", "pipeline": "orders"}`)
	createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypePipelineSuccess, config)

	triggers, err := tStore.FindTriggersByPipelineSuccess(ctx, "default", "bronze", "orders")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(triggers), 1)
	assert.Equal(t, domain.TriggerTypePipelineSuccess, triggers[0].Type)
}

func TestTriggerStore_FindTriggersByPipelineSuccess_NoMatch(t *testing.T) {
	pool := testPool(t)
	tStore := postgres.NewTriggerStore(pool)

	triggers, err := tStore.FindTriggersByPipelineSuccess(context.Background(), "nonexistent", "bronze", "x")
	require.NoError(t, err)
	assert.Empty(t, triggers)
}

func TestTriggerStore_FindTriggerByWebhookToken(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-find-webhook")
	tokenHash := "deadbeef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	config := json.RawMessage(fmt.Sprintf(`{"token_hash": %q}`, tokenHash))
	createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeWebhook, config)

	got, err := tStore.FindTriggerByWebhookToken(ctx, tokenHash)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, domain.TriggerTypeWebhook, got.Type)
}

func TestTriggerStore_FindTriggerByWebhookToken_NotFound(t *testing.T) {
	pool := testPool(t)
	tStore := postgres.NewTriggerStore(pool)

	got, err := tStore.FindTriggerByWebhookToken(context.Background(), "nonexistent_hash")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTriggerStore_FindTriggersByFilePattern(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-find-fp")
	config := json.RawMessage(`{"namespace": "default", "zone_name": "uploads", "pattern": "*.csv"}`)
	createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeFilePattern, config)

	triggers, err := tStore.FindTriggersByFilePattern(ctx, "default", "uploads")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(triggers), 1)
	assert.Equal(t, domain.TriggerTypeFilePattern, triggers[0].Type)
}

func TestTriggerStore_UpdateTriggerFired(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-fired")
	trigger := createTestTrigger(t, tStore, pipeline.ID, domain.TriggerTypeCron, json.RawMessage(`{"cron": "0 * * * *"}`))

	// Create a run to associate
	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "trigger"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	err := tStore.UpdateTriggerFired(ctx, trigger.ID.String(), run.ID)
	require.NoError(t, err)

	got, err := tStore.GetTrigger(ctx, trigger.ID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotNil(t, got.LastTriggeredAt)
	require.NotNil(t, got.LastRunID)
	assert.Equal(t, run.ID, *got.LastRunID)
}

func TestTriggerStore_DisabledTrigger_NotFoundByFinds(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	tStore := postgres.NewTriggerStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "t-disabled")
	config := json.RawMessage(`{"cron": "0 * * * *"}`)
	trigger := &domain.PipelineTrigger{
		PipelineID:      pipeline.ID,
		Type:            domain.TriggerTypeCron,
		Config:          config,
		Enabled:         false, // disabled
		CooldownSeconds: 60,
	}
	require.NoError(t, tStore.CreateTrigger(ctx, trigger))

	// FindTriggersByType only returns enabled triggers
	triggers, err := tStore.FindTriggersByType(ctx, string(domain.TriggerTypeCron))
	require.NoError(t, err)
	for _, tr := range triggers {
		assert.NotEqual(t, trigger.ID, tr.ID, "disabled trigger should not appear in FindTriggersByType")
	}
}

// ---------------------------------------------------------------------------
// HealthChecker tests
// ---------------------------------------------------------------------------

func TestHealthChecker_Ping(t *testing.T) {
	pool := testPool(t)
	checker := postgres.NewHealthChecker(pool)

	err := checker.HealthCheck(context.Background())
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// PipelineStore — additional operations
// ---------------------------------------------------------------------------

func TestPipelineStore_GetPipelineByID(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "by-id")
	require.NoError(t, store.CreatePipeline(ctx, p))

	got, err := store.GetPipelineByID(ctx, p.ID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, p.ID, got.ID)
	assert.Equal(t, "by-id", got.Name)
}

func TestPipelineStore_GetPipelineByID_InvalidUUID_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)

	got, err := store.GetPipelineByID(context.Background(), "not-a-uuid")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPipelineStore_GetPipelineByID_NotFound_ReturnsNil(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)

	got, err := store.GetPipelineByID(context.Background(), uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPipelineStore_CountPipelines(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", "count-1")))
	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "silver", "count-2")))
	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "gold", "count-3")))

	count, err := store.CountPipelines(ctx, api.PipelineFilter{})
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	count, err = store.CountPipelines(ctx, api.PipelineFilter{Layer: "silver"})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestPipelineStore_SetDraftDirty(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", "dirty-test")))

	got, err := store.GetPipeline(ctx, "default", "bronze", "dirty-test")
	require.NoError(t, err)
	assert.False(t, got.DraftDirty) // default is false

	err = store.SetDraftDirty(ctx, "default", "bronze", "dirty-test", true)
	require.NoError(t, err)

	got, err = store.GetPipeline(ctx, "default", "bronze", "dirty-test")
	require.NoError(t, err)
	assert.True(t, got.DraftDirty)

	// Set back to false
	err = store.SetDraftDirty(ctx, "default", "bronze", "dirty-test", false)
	require.NoError(t, err)

	got, err = store.GetPipeline(ctx, "default", "bronze", "dirty-test")
	require.NoError(t, err)
	assert.False(t, got.DraftDirty)
}

func TestPipelineStore_PublishPipeline(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", "publish-test")))

	versions := map[string]string{"pipeline.sql": "vid-123"}
	err := store.PublishPipeline(ctx, "default", "bronze", "publish-test", versions)
	require.NoError(t, err)

	got, err := store.GetPipeline(ctx, "default", "bronze", "publish-test")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotNil(t, got.PublishedAt)
	assert.Equal(t, "vid-123", got.PublishedVersions["pipeline.sql"])
	assert.False(t, got.DraftDirty)
}

func TestPipelineStore_ListPipelines_WithPagination(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, store.CreatePipeline(ctx, newTestPipeline("default", "bronze", fmt.Sprintf("paginated-%d", i))))
	}

	// First page
	page1, err := store.ListPipelines(ctx, api.PipelineFilter{Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, page1, 2)

	// Second page
	page2, err := store.ListPipelines(ctx, api.PipelineFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.Len(t, page2, 2)

	// No overlap
	assert.NotEqual(t, page1[0].ID, page2[0].ID)
}

func TestPipelineStore_SoftDeleteAndHardDelete(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "hard-delete")
	require.NoError(t, store.CreatePipeline(ctx, p))

	// Soft delete
	require.NoError(t, store.DeletePipeline(ctx, "default", "bronze", "hard-delete"))

	// Should appear in soft-deleted list
	deleted, err := store.ListSoftDeletedPipelines(ctx, time.Now().Add(1*time.Second))
	require.NoError(t, err)
	found := false
	for _, dp := range deleted {
		if dp.ID == p.ID {
			found = true
			assert.NotNil(t, dp.DeletedAt)
		}
	}
	assert.True(t, found, "pipeline should appear in soft-deleted list")

	// Hard delete
	err = store.HardDeletePipeline(ctx, p.ID)
	require.NoError(t, err)

	// Should no longer appear in soft-deleted list
	deleted, err = store.ListSoftDeletedPipelines(ctx, time.Now().Add(1*time.Second))
	require.NoError(t, err)
	for _, dp := range deleted {
		assert.NotEqual(t, p.ID, dp.ID, "pipeline should be gone after hard delete")
	}
}

func TestPipelineStore_UpdatePipelineRetention(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewPipelineStore(pool)
	ctx := context.Background()

	p := newTestPipeline("default", "bronze", "retention-test")
	require.NoError(t, store.CreatePipeline(ctx, p))

	retentionJSON := json.RawMessage(`{"runs_max_per_pipeline": 50, "runs_max_age_days": 30}`)
	err := store.UpdatePipelineRetention(ctx, p.ID, retentionJSON)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// RunStore — additional operations
// ---------------------------------------------------------------------------

func TestRunStore_CountRuns(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "count-runs")

	for i := 0; i < 3; i++ {
		require.NoError(t, rStore.CreateRun(ctx, &domain.Run{
			PipelineID: pipeline.ID,
			Status:     domain.RunStatusPending,
			Trigger:    "manual",
		}))
	}

	count, err := rStore.CountRuns(ctx, api.RunFilter{})
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestRunStore_SaveAndGetRunLogs(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "log-roundtrip")
	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusRunning, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	logs := []api.LogEntry{
		{Timestamp: "2024-01-01T00:00:00Z", Level: "INFO", Message: "Starting pipeline"},
		{Timestamp: "2024-01-01T00:00:01Z", Level: "INFO", Message: "Pipeline completed"},
	}
	err := rStore.SaveRunLogs(ctx, run.ID.String(), logs)
	require.NoError(t, err)

	got, err := rStore.GetRunLogs(ctx, run.ID.String())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Starting pipeline", got[0].Message)
	assert.Equal(t, "Pipeline completed", got[1].Message)
}

func TestRunStore_DeleteRunsBeyondLimit(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "beyond-limit")

	for i := 0; i < 5; i++ {
		require.NoError(t, rStore.CreateRun(ctx, &domain.Run{
			PipelineID: pipeline.ID,
			Status:     domain.RunStatusSuccess,
			Trigger:    "manual",
		}))
	}

	deleted, err := rStore.DeleteRunsBeyondLimit(ctx, pipeline.ID, 3)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)

	runs, err := rStore.ListRuns(ctx, api.RunFilter{PipelineID: pipeline.ID.String()})
	require.NoError(t, err)
	assert.Len(t, runs, 3)
}

func TestRunStore_DeleteRunsOlderThan(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "older-than")

	// Create a terminal run
	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run))
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusSuccess, nil, nil, nil))

	// Delete all terminal runs created before the future
	deleted, err := rStore.DeleteRunsOlderThan(ctx, time.Now().Add(1*time.Second))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, 1)
}

func TestRunStore_DeleteRunsOlderThan_SkipsPendingRuns(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "skip-pending")

	// Create a pending run (not terminal)
	require.NoError(t, rStore.CreateRun(ctx, &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    "manual",
	}))

	deleted, err := rStore.DeleteRunsOlderThan(ctx, time.Now().Add(1*time.Second))
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)
}

func TestRunStore_LatestRunPerPipeline(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	p1 := createTestPipeline(t, pStore, "default", "bronze", "latest-run-1")
	p2 := createTestPipeline(t, pStore, "default", "silver", "latest-run-2")

	// Create multiple runs per pipeline
	r1a := &domain.Run{PipelineID: p1.ID, Status: domain.RunStatusSuccess, Trigger: "manual"}
	r1b := &domain.Run{PipelineID: p1.ID, Status: domain.RunStatusRunning, Trigger: "schedule"}
	r2a := &domain.Run{PipelineID: p2.ID, Status: domain.RunStatusFailed, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, r1a))
	require.NoError(t, rStore.CreateRun(ctx, r1b))
	require.NoError(t, rStore.CreateRun(ctx, r2a))

	result, err := rStore.LatestRunPerPipeline(ctx, []uuid.UUID{p1.ID, p2.ID})
	require.NoError(t, err)
	assert.Len(t, result, 2)

	// p1's latest should be r1b (created last)
	latestP1, ok := result[p1.ID]
	require.True(t, ok)
	assert.Equal(t, r1b.ID, latestP1.ID)

	// p2's latest should be r2a
	latestP2, ok := result[p2.ID]
	require.True(t, ok)
	assert.Equal(t, r2a.ID, latestP2.ID)
}

func TestRunStore_LatestRunPerPipeline_EmptyInput(t *testing.T) {
	pool := testPool(t)
	rStore := postgres.NewRunStore(pool)

	result, err := rStore.LatestRunPerPipeline(context.Background(), []uuid.UUID{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRunStore_ListStuckRuns(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "stuck-runs")

	// Create a pending run
	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	// Stuck runs older than 1 second in the future — should find our run
	stuck, err := rStore.ListStuckRuns(ctx, time.Now().Add(1*time.Second))
	require.NoError(t, err)
	found := false
	for _, r := range stuck {
		if r.ID == run.ID {
			found = true
			assert.Equal(t, domain.RunStatusPending, r.Status)
		}
	}
	assert.True(t, found, "pending run should appear in stuck runs list")
}

func TestRunStore_ListStuckRuns_ExcludesTerminal(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "not-stuck")

	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run))
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusSuccess, nil, nil, nil))

	stuck, err := rStore.ListStuckRuns(ctx, time.Now().Add(1*time.Second))
	require.NoError(t, err)
	for _, r := range stuck {
		assert.NotEqual(t, run.ID, r.ID, "terminal run should not appear in stuck runs")
	}
}

func TestRunStore_UpdateRunStatus_WithError(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "err-status")
	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	errMsg := "OOM: DuckDB ran out of memory"
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusFailed, &errMsg, nil, nil))

	got, err := rStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	assert.Equal(t, domain.RunStatusFailed, got.Status)
	require.NotNil(t, got.Error)
	assert.Equal(t, "OOM: DuckDB ran out of memory", *got.Error)
}

func TestRunStore_UpdateRunStatus_WithRowsWritten(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "rows-written")
	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "manual"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusRunning, nil, nil, nil))

	var durationMs int64 = 1500
	var rowsWritten int64 = 42000
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusSuccess, nil, &durationMs, &rowsWritten))

	got, err := rStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	assert.Equal(t, domain.RunStatusSuccess, got.Status)
	require.NotNil(t, got.DurationMs)
	assert.Equal(t, 1500, *got.DurationMs)
	require.NotNil(t, got.RowsWritten)
	assert.Equal(t, int64(42000), *got.RowsWritten)
}

func TestRunStore_ListRuns_WithPagination(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "run-pagination")
	for i := 0; i < 5; i++ {
		require.NoError(t, rStore.CreateRun(ctx, &domain.Run{
			PipelineID: pipeline.ID,
			Status:     domain.RunStatusPending,
			Trigger:    "manual",
		}))
	}

	page1, err := rStore.ListRuns(ctx, api.RunFilter{Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, page1, 2)

	page2, err := rStore.ListRuns(ctx, api.RunFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.Len(t, page2, 2)

	assert.NotEqual(t, page1[0].ID, page2[0].ID)
}

// ---------------------------------------------------------------------------
// ScheduleStore — additional operations
// ---------------------------------------------------------------------------

func TestScheduleStore_UpdateScheduleRun(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	sStore := postgres.NewScheduleStore(pool)
	rStore := postgres.NewRunStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "sched-run")
	sched := &domain.Schedule{PipelineID: pipeline.ID, CronExpr: "0 * * * *", Enabled: true}
	require.NoError(t, sStore.CreateSchedule(ctx, sched))

	run := &domain.Run{PipelineID: pipeline.ID, Status: domain.RunStatusPending, Trigger: "schedule:hourly"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	lastRunAt := time.Now().UTC().Truncate(time.Second)
	nextRunAt := lastRunAt.Add(1 * time.Hour)

	err := sStore.UpdateScheduleRun(ctx, sched.ID.String(), run.ID.String(), lastRunAt, nextRunAt)
	require.NoError(t, err)

	got, err := sStore.GetSchedule(ctx, sched.ID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.LastRunAt)
	require.NotNil(t, got.NextRunAt)
	require.NotNil(t, got.LastRunID)
	assert.Equal(t, run.ID, *got.LastRunID)
}

func TestScheduleStore_UpdateSchedule_EnableDisable(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	sStore := postgres.NewScheduleStore(pool)
	ctx := context.Background()

	pipeline := createTestPipeline(t, pStore, "default", "bronze", "sched-toggle")
	sched := &domain.Schedule{PipelineID: pipeline.ID, CronExpr: "0 * * * *", Enabled: true}
	require.NoError(t, sStore.CreateSchedule(ctx, sched))

	// Disable
	disabled := false
	updated, err := sStore.UpdateSchedule(ctx, sched.ID.String(), api.UpdateScheduleRequest{
		Enabled: &disabled,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.Enabled)

	// Re-enable
	enabled := true
	updated, err = sStore.UpdateSchedule(ctx, sched.ID.String(), api.UpdateScheduleRequest{
		Enabled: &enabled,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.True(t, updated.Enabled)
}

// ---------------------------------------------------------------------------
// NamespaceStore — additional operations
// ---------------------------------------------------------------------------

func TestNamespaceStore_CreateDuplicate_ReturnsError(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	err := store.CreateNamespace(ctx, "dup-ns", nil)
	require.NoError(t, err)

	err = store.CreateNamespace(ctx, "dup-ns", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestNamespaceStore_UpdateNamespace(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	err := store.CreateNamespace(ctx, "updatable-ns", nil)
	require.NoError(t, err)

	err = store.UpdateNamespace(ctx, "updatable-ns", "A namespace for analytics")
	require.NoError(t, err)

	namespaces, err := store.ListNamespaces(ctx)
	require.NoError(t, err)
	for _, ns := range namespaces {
		if ns.Name == "updatable-ns" {
			assert.Equal(t, "A namespace for analytics", ns.Description)
		}
	}
}

func TestNamespaceStore_CreateWithCreatedBy(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	creator := "admin-user"
	err := store.CreateNamespace(ctx, "attributed-ns", &creator)
	require.NoError(t, err)

	namespaces, err := store.ListNamespaces(ctx)
	require.NoError(t, err)
	for _, ns := range namespaces {
		if ns.Name == "attributed-ns" {
			require.NotNil(t, ns.CreatedBy)
			assert.Equal(t, "admin-user", *ns.CreatedBy)
		}
	}
}

func TestNamespaceStore_DeleteDefault_NoError(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewNamespaceStore(pool)
	ctx := context.Background()

	// Deleting "default" should execute without error, but pipelines
	// referencing it via FK might prevent actual deletion in practice.
	// Here we test the store method works at the SQL level when no
	// FK constraints block it.
	err := store.CreateNamespace(ctx, "deletable-ns", nil)
	require.NoError(t, err)

	err = store.DeleteNamespace(ctx, "deletable-ns")
	require.NoError(t, err)

	namespaces, err := store.ListNamespaces(ctx)
	require.NoError(t, err)
	for _, ns := range namespaces {
		assert.NotEqual(t, "deletable-ns", ns.Name)
	}
}

// ---------------------------------------------------------------------------
// LandingZoneStore — additional operations
// ---------------------------------------------------------------------------

func TestLandingZoneStore_UpdateZone(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{Namespace: "default", Name: "updatable-zone", Description: "original"}
	require.NoError(t, store.CreateZone(ctx, z))

	newDesc := "updated description"
	updated, err := store.UpdateZone(ctx, "default", "updatable-zone", &newDesc, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "updated description", updated.Description)
}

func TestLandingZoneStore_UpdateZoneLifecycle(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{Namespace: "default", Name: "lifecycle-zone"}
	require.NoError(t, store.CreateZone(ctx, z))

	maxAge := 7
	autoPurge := true
	err := store.UpdateZoneLifecycle(ctx, z.ID, &maxAge, &autoPurge)
	require.NoError(t, err)
}

func TestLandingZoneStore_ListZonesWithAutoPurge(t *testing.T) {
	pool := testPool(t)
	store := postgres.NewLandingZoneStore(pool)
	ctx := context.Background()

	z := &domain.LandingZone{Namespace: "default", Name: "purge-zone"}
	require.NoError(t, store.CreateZone(ctx, z))

	autoPurge := true
	maxAge := 14
	require.NoError(t, store.UpdateZoneLifecycle(ctx, z.ID, &maxAge, &autoPurge))

	zones, err := store.ListZonesWithAutoPurge(ctx)
	require.NoError(t, err)
	found := false
	for _, zone := range zones {
		if zone.ID == z.ID {
			found = true
			assert.True(t, zone.AutoPurge)
			require.NotNil(t, zone.ProcessedMaxAgeDays)
			assert.Equal(t, 14, *zone.ProcessedMaxAgeDays)
		}
	}
	assert.True(t, found, "zone with auto_purge should appear in list")
}

// ---------------------------------------------------------------------------
// Cross-store integration: pipeline + run + schedule lifecycle
// ---------------------------------------------------------------------------

func TestFullLifecycle_Pipeline_Run_Schedule(t *testing.T) {
	pool := testPool(t)
	pStore := postgres.NewPipelineStore(pool)
	rStore := postgres.NewRunStore(pool)
	sStore := postgres.NewScheduleStore(pool)
	ctx := context.Background()

	// 1. Create pipeline
	p := newTestPipeline("default", "bronze", "lifecycle-test")
	require.NoError(t, pStore.CreatePipeline(ctx, p))

	// 2. Create schedule
	sched := &domain.Schedule{PipelineID: p.ID, CronExpr: "0 * * * *", Enabled: true}
	require.NoError(t, sStore.CreateSchedule(ctx, sched))

	// 3. Create run (simulating scheduler trigger)
	run := &domain.Run{PipelineID: p.ID, Status: domain.RunStatusPending, Trigger: "schedule:hourly"}
	require.NoError(t, rStore.CreateRun(ctx, run))

	// 4. Progress run through lifecycle
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusRunning, nil, nil, nil))

	var durationMs int64 = 5000
	var rowsWritten int64 = 1000
	require.NoError(t, rStore.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusSuccess, nil, &durationMs, &rowsWritten))

	// 5. Update schedule with run info
	now := time.Now().UTC().Truncate(time.Second)
	nextRun := now.Add(1 * time.Hour)
	require.NoError(t, sStore.UpdateScheduleRun(ctx, sched.ID.String(), run.ID.String(), now, nextRun))

	// 6. Publish the pipeline
	versions := map[string]string{"pipeline.sql": "vid-final"}
	require.NoError(t, pStore.PublishPipeline(ctx, "default", "bronze", "lifecycle-test", versions))

	// Verify final state
	finalPipeline, err := pStore.GetPipeline(ctx, "default", "bronze", "lifecycle-test")
	require.NoError(t, err)
	assert.NotNil(t, finalPipeline.PublishedAt)
	assert.Equal(t, "vid-final", finalPipeline.PublishedVersions["pipeline.sql"])

	finalRun, err := rStore.GetRun(ctx, run.ID.String())
	require.NoError(t, err)
	assert.Equal(t, domain.RunStatusSuccess, finalRun.Status)
	assert.NotNil(t, finalRun.StartedAt)
	assert.NotNil(t, finalRun.FinishedAt)
	require.NotNil(t, finalRun.DurationMs)
	assert.Equal(t, 5000, *finalRun.DurationMs)
	require.NotNil(t, finalRun.RowsWritten)
	assert.Equal(t, int64(1000), *finalRun.RowsWritten)

	finalSched, err := sStore.GetSchedule(ctx, sched.ID.String())
	require.NoError(t, err)
	require.NotNil(t, finalSched.LastRunID)
	assert.Equal(t, run.ID, *finalSched.LastRunID)
}
