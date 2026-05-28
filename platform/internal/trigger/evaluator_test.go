package trigger

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Test doubles
// ----------------------------------------------------------------------------

// raceTriggerStore is a thread-safe in-memory PipelineTriggerStore that
// implements CAS semantics identically to the Postgres backend, so the
// evaluator's race-prevention logic can be unit-tested without a database.
type raceTriggerStore struct {
	mu       sync.Mutex
	triggers []domain.PipelineTrigger
	// firedCount counts successful UpdateTriggerFiredCAS calls — the test
	// asserts this is exactly 1 when two goroutines race on the same trigger.
	firedCount int
}

func (s *raceTriggerStore) addTrigger(t domain.PipelineTrigger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggers = append(s.triggers, t)
}

func (s *raceTriggerStore) ListTriggers(_ context.Context, _ uuid.UUID) ([]domain.PipelineTrigger, error) {
	return nil, nil
}

func (s *raceTriggerStore) GetTrigger(_ context.Context, _ string) (*domain.PipelineTrigger, error) {
	return nil, nil
}

func (s *raceTriggerStore) CreateTrigger(_ context.Context, _ *domain.PipelineTrigger) error {
	return nil
}

func (s *raceTriggerStore) UpdateTrigger(_ context.Context, _ string, _ api.UpdateTriggerRequest) (*domain.PipelineTrigger, error) {
	return nil, nil
}

func (s *raceTriggerStore) DeleteTrigger(_ context.Context, _ string) error { return nil }

func (s *raceTriggerStore) FindTriggersByLandingZone(_ context.Context, _, _ string) ([]domain.PipelineTrigger, error) {
	return nil, nil
}

func (s *raceTriggerStore) FindTriggersByType(_ context.Context, triggerType string) ([]domain.PipelineTrigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.PipelineTrigger
	for _, t := range s.triggers {
		if string(t.Type) == triggerType && t.Enabled {
			cp := t
			if t.LastTriggeredAt != nil {
				stamp := *t.LastTriggeredAt
				cp.LastTriggeredAt = &stamp
			}
			out = append(out, cp)
		}
	}
	return out, nil
}

func (s *raceTriggerStore) FindTriggerByWebhookToken(_ context.Context, _ string) (*domain.PipelineTrigger, error) {
	return nil, nil
}

func (s *raceTriggerStore) FindTriggersByPipelineSuccess(_ context.Context, _, _, _ string) ([]domain.PipelineTrigger, error) {
	return nil, nil
}

func (s *raceTriggerStore) FindTriggersByFilePattern(_ context.Context, _, _ string) ([]domain.PipelineTrigger, error) {
	return nil, nil
}

func (s *raceTriggerStore) UpdateTriggerFired(_ context.Context, triggerID string, runID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return err
	}
	for i, t := range s.triggers {
		if t.ID == uid {
			now := time.Now()
			s.triggers[i].LastTriggeredAt = &now
			s.triggers[i].LastRunID = &runID
			return nil
		}
	}
	_ = uid
	return nil
}

func (s *raceTriggerStore) UpdateTriggerFiredCAS(
	_ context.Context,
	triggerID string,
	newTriggeredAt time.Time,
	runID uuid.UUID,
	expectedPrev *time.Time,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return false, err
	}
	for i, t := range s.triggers {
		if t.ID != uid {
			continue
		}
		current := s.triggers[i].LastTriggeredAt
		match := (current == nil && expectedPrev == nil) ||
			(current != nil && expectedPrev != nil && current.Equal(*expectedPrev))
		if !match {
			return false, nil
		}
		stamp := newTriggeredAt
		s.triggers[i].LastTriggeredAt = &stamp
		s.triggers[i].LastRunID = &runID
		s.firedCount++
		_ = t
		return true, nil
	}
	return false, nil
}

// stubPipelineStore is a no-op PipelineStore that returns a fixed pipeline
// for ID lookups. Only GetPipelineByID is exercised in this test; the rest
// satisfy the interface.
type stubPipelineStore struct {
	pipeline *domain.Pipeline
}

func (s *stubPipelineStore) ListPipelines(_ context.Context, _ api.PipelineFilter) ([]domain.Pipeline, error) {
	return nil, nil
}
func (s *stubPipelineStore) CountPipelines(_ context.Context, _ api.PipelineFilter) (int, error) {
	return 0, nil
}
func (s *stubPipelineStore) GetPipeline(_ context.Context, _, _, _ string) (*domain.Pipeline, error) {
	return nil, nil
}
func (s *stubPipelineStore) GetPipelineByID(_ context.Context, _ string) (*domain.Pipeline, error) {
	return s.pipeline, nil
}
func (s *stubPipelineStore) CreatePipeline(_ context.Context, _ *domain.Pipeline) error {
	return nil
}
func (s *stubPipelineStore) UpdatePipeline(_ context.Context, _, _, _ string, _ api.UpdatePipelineRequest) (*domain.Pipeline, error) {
	return nil, nil
}
func (s *stubPipelineStore) DeletePipeline(_ context.Context, _, _, _ string) error { return nil }
func (s *stubPipelineStore) SetDraftDirty(_ context.Context, _, _, _ string, _ bool) error {
	return nil
}
func (s *stubPipelineStore) PublishPipeline(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}
func (s *stubPipelineStore) UpdatePipelineRetention(_ context.Context, _ uuid.UUID, _ json.RawMessage) error {
	return nil
}
func (s *stubPipelineStore) ListSoftDeletedPipelines(_ context.Context, _ time.Time) ([]domain.Pipeline, error) {
	return nil, nil
}
func (s *stubPipelineStore) HardDeletePipeline(_ context.Context, _ uuid.UUID) error { return nil }

// raceRunStore is a thread-safe in-memory RunStore. ListRuns returns a fixed
// set of "dependency runs" so evaluateCronDependency sees new upstream data,
// and CreateRun records calls so the test can assert exactly one fire.
type raceRunStore struct {
	mu             sync.Mutex
	created        []*domain.Run
	dependencyRuns []domain.Run
}

func (s *raceRunStore) ListRuns(_ context.Context, _ api.RunFilter) ([]domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Run(nil), s.dependencyRuns...), nil
}
func (s *raceRunStore) CountRuns(_ context.Context, _ api.RunFilter) (int, error) { return 0, nil }
func (s *raceRunStore) GetRun(_ context.Context, _ string) (*domain.Run, error)   { return nil, nil }
func (s *raceRunStore) CreateRun(_ context.Context, run *domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run.ID = uuid.New()
	s.created = append(s.created, run)
	return nil
}
func (s *raceRunStore) UpdateRunStatus(_ context.Context, _ string, _ domain.RunStatus, _ *string, _ *int64, _ *int64) error {
	return nil
}
func (s *raceRunStore) GetRunLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}
func (s *raceRunStore) SaveRunLogs(_ context.Context, _ string, _ []api.LogEntry) error { return nil }
func (s *raceRunStore) DeleteRunsBeyondLimit(_ context.Context, _ uuid.UUID, _ int) (int, error) {
	return 0, nil
}
func (s *raceRunStore) DeleteRunsOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}
func (s *raceRunStore) ListStuckRuns(_ context.Context, _ time.Time) ([]domain.Run, error) {
	return nil, nil
}
func (s *raceRunStore) ListStuckPendingRuns(_ context.Context, _ time.Time) ([]domain.Run, error) {
	return nil, nil
}
func (s *raceRunStore) LatestRunPerPipeline(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]*domain.Run, error) {
	return nil, nil
}

// raceExecutor records every Submit call so the test can assert the count.
// The remaining Executor methods are no-ops — they exist only to satisfy
// the interface.
type raceExecutor struct {
	mu    sync.Mutex
	calls int
}

func (e *raceExecutor) Submit(_ context.Context, _ *domain.Run, _ *domain.Pipeline) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return nil
}
func (e *raceExecutor) Cancel(_ context.Context, _ string) error { return nil }
func (e *raceExecutor) GetLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}
func (e *raceExecutor) Preview(_ context.Context, _ *domain.Pipeline, _ int, _ []string, _ string) (*api.PreviewResult, error) {
	return nil, nil
}
func (e *raceExecutor) ValidatePipeline(_ context.Context, _ *domain.Pipeline) (*api.ValidationResult, error) {
	return nil, nil
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// TestEvaluator_ConcurrentTickAndEvent_OnlyOneFire is the regression test for
// the duplicate-run race fixed by the CAS update. We construct a single
// cron_dependency trigger that is overdue (last_triggered_at well in the past;
// the dependency has new successful data), then call evaluateCronDependency
// from two goroutines simultaneously — simulating the tick() and
// handleRunCompleted paths firing at the same instant.
//
// Without CAS, both goroutines would see "due", both would call
// CreateRun + Submit, and the executor would see 2 calls.
//
// With CAS, only the first claim wins; the second's UPDATE matches 0 rows
// and the evaluator silently skips. The executor sees exactly 1 call.
func TestEvaluator_ConcurrentTickAndEvent_OnlyOneFire(t *testing.T) {
	pipelineID := uuid.New()
	triggerID := uuid.New()
	pastFire := time.Now().Add(-2 * time.Hour)
	depFinished := time.Now().Add(-1 * time.Minute) // newer than pastFire — has new data

	triggers := &raceTriggerStore{}
	triggers.addTrigger(domain.PipelineTrigger{
		ID:              triggerID,
		PipelineID:      pipelineID,
		Type:            domain.TriggerTypeCronDependency,
		Config:          json.RawMessage(`{"cron_expr":"* * * * *","dependencies":["default.bronze.upstream"]}`),
		Enabled:         true,
		LastTriggeredAt: &pastFire,
	})

	pipelines := &stubPipelineStore{pipeline: &domain.Pipeline{
		ID:        pipelineID,
		Namespace: "default",
		Layer:     domain.Layer("silver"),
		Name:      "downstream",
	}}

	runs := &raceRunStore{
		dependencyRuns: []domain.Run{
			{
				ID:         uuid.New(),
				PipelineID: uuid.New(),
				Status:     domain.RunStatusSuccess,
				FinishedAt: &depFinished,
			},
		},
	}

	exec := &raceExecutor{}

	eval := NewEvaluator(triggers, pipelines, runs, exec, time.Minute)

	// Snapshot the trigger as each path would observe it (same expectedPrev).
	observed, err := triggers.FindTriggersByType(context.Background(), string(domain.TriggerTypeCronDependency))
	require.NoError(t, err)
	require.Len(t, observed, 1)
	tickView := observed[0]
	eventView := observed[0]
	now := time.Now()

	// Fire both paths simultaneously.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		eval.evaluateCronDependency(context.Background(), tickView, now)
	}()
	go func() {
		defer wg.Done()
		eval.evaluateCronDependency(context.Background(), eventView, now)
	}()
	wg.Wait()

	triggers.mu.Lock()
	firedCount := triggers.firedCount
	triggers.mu.Unlock()
	assert.Equal(t, 1, firedCount, "CAS must allow exactly one fire across racing paths")

	runs.mu.Lock()
	createdCount := len(runs.created)
	runs.mu.Unlock()
	assert.Equal(t, 1, createdCount, "exactly one run created — race-loser must skip")

	exec.mu.Lock()
	execCalls := exec.calls
	exec.mu.Unlock()
	assert.Equal(t, 1, execCalls, "executor must see exactly one Submit")
}
