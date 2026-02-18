package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock stores ---

type mockScheduleStore struct {
	mu        sync.Mutex
	schedules []domain.Schedule
	updated   map[string]scheduleRunUpdate // schedule_id -> last update
}

type scheduleRunUpdate struct {
	lastRunID string
	lastRunAt time.Time
	nextRunAt time.Time
}

func newMockScheduleStore() *mockScheduleStore {
	return &mockScheduleStore{
		updated: make(map[string]scheduleRunUpdate),
	}
}

func (m *mockScheduleStore) ListSchedules(_ context.Context) ([]domain.Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]domain.Schedule, len(m.schedules))
	copy(result, m.schedules)
	return result, nil
}

func (m *mockScheduleStore) GetSchedule(_ context.Context, id string) (*domain.Schedule, error) {
	return nil, nil
}

func (m *mockScheduleStore) CreateSchedule(_ context.Context, schedule *domain.Schedule) error {
	return nil
}

func (m *mockScheduleStore) UpdateSchedule(_ context.Context, id string, update api.UpdateScheduleRequest) (*domain.Schedule, error) {
	return nil, nil
}

func (m *mockScheduleStore) UpdateScheduleRun(_ context.Context, id string, lastRunID string, lastRunAt time.Time, nextRunAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updated[id] = scheduleRunUpdate{
		lastRunID: lastRunID,
		lastRunAt: lastRunAt,
		nextRunAt: nextRunAt,
	}
	return nil
}

func (m *mockScheduleStore) DeleteSchedule(_ context.Context, id string) error {
	return nil
}

func (m *mockScheduleStore) getUpdate(id string) (scheduleRunUpdate, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.updated[id]
	return u, ok
}

type mockPipelineStore struct {
	pipelines map[string]*domain.Pipeline // id -> pipeline
}

func newMockPipelineStore() *mockPipelineStore {
	return &mockPipelineStore{pipelines: make(map[string]*domain.Pipeline)}
}

func (m *mockPipelineStore) ListPipelines(_ context.Context, _ api.PipelineFilter) ([]domain.Pipeline, error) {
	return nil, nil
}

func (m *mockPipelineStore) CountPipelines(_ context.Context, _ api.PipelineFilter) (int, error) {
	return 0, nil
}

func (m *mockPipelineStore) GetPipeline(_ context.Context, _, _, _ string) (*domain.Pipeline, error) {
	return nil, nil
}

func (m *mockPipelineStore) GetPipelineByID(_ context.Context, id string) (*domain.Pipeline, error) {
	p, ok := m.pipelines[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (m *mockPipelineStore) CreatePipeline(_ context.Context, p *domain.Pipeline) error {
	return nil
}

func (m *mockPipelineStore) UpdatePipeline(_ context.Context, _, _, _ string, _ api.UpdatePipelineRequest) (*domain.Pipeline, error) {
	return nil, nil
}

func (m *mockPipelineStore) DeletePipeline(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockPipelineStore) UpdatePipelineRetention(_ context.Context, _ uuid.UUID, _ json.RawMessage) error {
	return nil
}

func (m *mockPipelineStore) ListSoftDeletedPipelines(_ context.Context, _ time.Time) ([]domain.Pipeline, error) {
	return nil, nil
}

func (m *mockPipelineStore) HardDeletePipeline(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockPipelineStore) SetDraftDirty(_ context.Context, _, _, _ string, _ bool) error {
	return nil
}

func (m *mockPipelineStore) PublishPipeline(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}

type mockRunStore struct {
	mu   sync.Mutex
	runs []domain.Run
}

func newMockRunStore() *mockRunStore {
	return &mockRunStore{}
}

func (m *mockRunStore) ListRuns(_ context.Context, filter api.RunFilter) ([]domain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []domain.Run
	for _, r := range m.runs {
		if filter.PipelineID != "" && r.PipelineID.String() != filter.PipelineID {
			continue
		}
		if filter.Status != "" && string(r.Status) != filter.Status {
			continue
		}
		result = append(result, r)
		if filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}
	return result, nil
}

func (m *mockRunStore) CountRuns(_ context.Context, _ api.RunFilter) (int, error) {
	return 0, nil
}

func (m *mockRunStore) GetRun(_ context.Context, _ string) (*domain.Run, error) {
	return nil, nil
}

func (m *mockRunStore) CreateRun(_ context.Context, run *domain.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	run.ID = uuid.New()
	m.runs = append(m.runs, *run)
	return nil
}

func (m *mockRunStore) UpdateRunStatus(_ context.Context, _ string, _ domain.RunStatus, _ *string, _ *int64, _ *int64) error {
	return nil
}

func (m *mockRunStore) SaveRunLogs(_ context.Context, _ string, _ []api.LogEntry) error {
	return nil
}

func (m *mockRunStore) GetRunLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}

func (m *mockRunStore) DeleteRunsBeyondLimit(_ context.Context, _ uuid.UUID, _ int) (int, error) {
	return 0, nil
}

func (m *mockRunStore) DeleteRunsOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func (m *mockRunStore) ListStuckRuns(_ context.Context, _ time.Time) ([]domain.Run, error) {
	return nil, nil
}

func (m *mockRunStore) LatestRunPerPipeline(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]*domain.Run, error) {
	return nil, nil
}

func (m *mockRunStore) getRuns() []domain.Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]domain.Run, len(m.runs))
	copy(result, m.runs)
	return result
}

type mockExecutor struct {
	mu       sync.Mutex
	submits  []submitCall
	submitFn func(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error
}

type submitCall struct {
	runID      uuid.UUID
	pipelineID uuid.UUID
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{}
}

func (m *mockExecutor) Submit(ctx context.Context, run *domain.Run, pipeline *domain.Pipeline) error {
	m.mu.Lock()
	m.submits = append(m.submits, submitCall{runID: run.ID, pipelineID: pipeline.ID})
	m.mu.Unlock()
	if m.submitFn != nil {
		return m.submitFn(ctx, run, pipeline)
	}
	return nil
}

func (m *mockExecutor) Cancel(_ context.Context, _ string) error {
	return nil
}

func (m *mockExecutor) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}

func (m *mockExecutor) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockExecutor) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return &api.ValidationResult{Valid: true}, nil
}

func (m *mockExecutor) getSubmits() []submitCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]submitCall, len(m.submits))
	copy(result, m.submits)
	return result
}

// --- Tests ---

func TestTick_NoSchedules_DoesNothing(t *testing.T) {
	schedStore := newMockScheduleStore()
	runStore := newMockRunStore()
	exec := newMockExecutor()

	sched := New(schedStore, newMockPipelineStore(), runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	assert.Empty(t, runStore.getRuns())
	assert.Empty(t, exec.getSubmits())
}

func TestTick_DisabledSchedule_Skipped(t *testing.T) {
	pipelineID := uuid.New()
	schedStore := newMockScheduleStore()
	past := time.Now().Add(-1 * time.Hour)
	schedStore.schedules = []domain.Schedule{
		{
			ID:         uuid.New(),
			PipelineID: pipelineID,
			CronExpr:   "* * * * *",
			Enabled:    false,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{ID: pipelineID}

	runStore := newMockRunStore()
	exec := newMockExecutor()

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	assert.Empty(t, runStore.getRuns())
	assert.Empty(t, exec.getSubmits())
}

func TestTick_DueSchedule_CreatesRun(t *testing.T) {
	pipelineID := uuid.New()
	schedID := uuid.New()
	past := time.Now().Add(-5 * time.Minute)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "* * * * *", // every minute
			Enabled:    true,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
	}

	runStore := newMockRunStore()
	exec := newMockExecutor()

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	runs := runStore.getRuns()
	require.Len(t, runs, 1)
	assert.Equal(t, pipelineID, runs[0].PipelineID)
	assert.Equal(t, "schedule:* * * * *", runs[0].Trigger)

	submits := exec.getSubmits()
	require.Len(t, submits, 1)
	assert.Equal(t, pipelineID, submits[0].pipelineID)

	update, ok := schedStore.getUpdate(schedID.String())
	require.True(t, ok)
	assert.NotEmpty(t, update.lastRunID)
	assert.True(t, update.nextRunAt.After(time.Now().Add(-1*time.Second)))
}

func TestTick_FutureSchedule_NotFired(t *testing.T) {
	pipelineID := uuid.New()
	future := time.Now().Add(1 * time.Hour)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         uuid.New(),
			PipelineID: pipelineID,
			CronExpr:   "0 0 * * *",
			Enabled:    true,
			NextRunAt:  &future,
		},
	}

	runStore := newMockRunStore()
	exec := newMockExecutor()

	sched := New(schedStore, newMockPipelineStore(), runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	assert.Empty(t, runStore.getRuns())
	assert.Empty(t, exec.getSubmits())
}

func TestTick_NilNextRunAt_ComputesIt(t *testing.T) {
	schedID := uuid.New()
	pipelineID := uuid.New()

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "0 * * * *", // hourly
			Enabled:    true,
			NextRunAt:  nil,
		},
	}

	runStore := newMockRunStore()
	exec := newMockExecutor()

	sched := New(schedStore, newMockPipelineStore(), runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Should NOT create a run
	assert.Empty(t, runStore.getRuns())
	assert.Empty(t, exec.getSubmits())

	// Should set next_run_at
	update, ok := schedStore.getUpdate(schedID.String())
	require.True(t, ok)
	assert.True(t, update.nextRunAt.After(time.Now()))
	assert.Empty(t, update.lastRunID) // no run was fired
}

func TestTick_MissedSchedule_FiresOnce(t *testing.T) {
	pipelineID := uuid.New()
	schedID := uuid.New()

	// Missed by 3 hours
	past := time.Now().Add(-3 * time.Hour)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "0 * * * *", // hourly
			Enabled:    true,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.LayerBronze,
		Name:      "events",
	}

	runStore := newMockRunStore()
	exec := newMockExecutor()

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Should fire exactly once (catch up once, not 3 times)
	runs := runStore.getRuns()
	require.Len(t, runs, 1)

	// Next run should be in the future
	update, ok := schedStore.getUpdate(schedID.String())
	require.True(t, ok)
	assert.True(t, update.nextRunAt.After(time.Now().Add(-1*time.Second)))
}

func TestTick_ExecutorFails_LogsAndContinues(t *testing.T) {
	pipelineID := uuid.New()
	schedID := uuid.New()
	past := time.Now().Add(-5 * time.Minute)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "* * * * *",
			Enabled:    true,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
	}

	runStore := newMockRunStore()
	exec := newMockExecutor()
	exec.submitFn = func(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error {
		return fmt.Errorf("runner unavailable")
	}

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Run should still be created (even though executor failed)
	runs := runStore.getRuns()
	require.Len(t, runs, 1)

	// Schedule should still be updated (next_run_at advanced)
	update, ok := schedStore.getUpdate(schedID.String())
	require.True(t, ok)
	assert.NotEmpty(t, update.lastRunID)
}

func TestTick_InvalidCron_SkipsWithLog(t *testing.T) {
	schedStore := newMockScheduleStore()
	past := time.Now().Add(-5 * time.Minute)
	schedStore.schedules = []domain.Schedule{
		{
			ID:        uuid.New(),
			CronExpr:  "not a valid cron",
			Enabled:   true,
			NextRunAt: &past,
		},
	}

	runStore := newMockRunStore()
	exec := newMockExecutor()

	sched := New(schedStore, newMockPipelineStore(), runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Should not create any runs
	assert.Empty(t, runStore.getRuns())
	assert.Empty(t, exec.getSubmits())
}

func TestTick_PipelineWithActiveRun_Skipped(t *testing.T) {
	pipelineID := uuid.New()
	schedID := uuid.New()
	past := time.Now().Add(-5 * time.Minute)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "* * * * *",
			Enabled:    true,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
	}

	// Pre-seed a running run for this pipeline
	runStore := newMockRunStore()
	runStore.runs = []domain.Run{
		{
			ID:         uuid.New(),
			PipelineID: pipelineID,
			Status:     domain.RunStatusRunning,
			Trigger:    "schedule:* * * * *",
		},
	}
	exec := newMockExecutor()

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Should NOT create a new run — pipeline already has one running
	assert.Len(t, runStore.getRuns(), 1, "no new run should be created")
	assert.Empty(t, exec.getSubmits(), "executor should not be called")

	// Schedule should NOT be updated (no advance of next_run_at)
	_, ok := schedStore.getUpdate(schedID.String())
	assert.False(t, ok, "schedule should not be updated when skipped")
}

func TestTick_PipelineWithPendingRun_Skipped(t *testing.T) {
	pipelineID := uuid.New()
	schedID := uuid.New()
	past := time.Now().Add(-5 * time.Minute)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "* * * * *",
			Enabled:    true,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
	}

	// Pre-seed a pending run for this pipeline
	runStore := newMockRunStore()
	runStore.runs = []domain.Run{
		{
			ID:         uuid.New(),
			PipelineID: pipelineID,
			Status:     domain.RunStatusPending,
			Trigger:    "schedule:* * * * *",
		},
	}
	exec := newMockExecutor()

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Should NOT create a new run
	assert.Len(t, runStore.getRuns(), 1, "no new run should be created")
	assert.Empty(t, exec.getSubmits())
}

func TestTick_PipelineWithTerminalRun_NotSkipped(t *testing.T) {
	pipelineID := uuid.New()
	schedID := uuid.New()
	past := time.Now().Add(-5 * time.Minute)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "* * * * *",
			Enabled:    true,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
	}

	// Pre-seed a completed (terminal) run — should NOT block new runs
	runStore := newMockRunStore()
	runStore.runs = []domain.Run{
		{
			ID:         uuid.New(),
			PipelineID: pipelineID,
			Status:     domain.RunStatusSuccess,
			Trigger:    "schedule:* * * * *",
		},
	}
	exec := newMockExecutor()

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Should create a new run because the existing one is terminal
	runs := runStore.getRuns()
	assert.Len(t, runs, 2, "new run should be created when only terminal runs exist")
	assert.Len(t, exec.getSubmits(), 1)
}

func TestTick_RunnerBusy_DoesNotAdvanceSchedule(t *testing.T) {
	pipelineID := uuid.New()
	schedID := uuid.New()
	past := time.Now().Add(-5 * time.Minute)

	schedStore := newMockScheduleStore()
	schedStore.schedules = []domain.Schedule{
		{
			ID:         schedID,
			PipelineID: pipelineID,
			CronExpr:   "* * * * *",
			Enabled:    true,
			NextRunAt:  &past,
		},
	}

	pipelineStore := newMockPipelineStore()
	pipelineStore.pipelines[pipelineID.String()] = &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
	}

	runStore := newMockRunStore()
	exec := newMockExecutor()
	exec.submitFn = func(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error {
		return fmt.Errorf("submit pipeline: %w", executor.ErrRunnerBusy)
	}

	sched := New(schedStore, pipelineStore, runStore, exec, 30*time.Second)
	sched.tick(context.Background())

	// Run should be created (in pending state)
	runs := runStore.getRuns()
	require.Len(t, runs, 1)

	// Schedule should NOT be updated (no advance) — will retry next tick
	_, ok := schedStore.getUpdate(schedID.String())
	assert.False(t, ok, "schedule should NOT advance when runner is busy")
}
