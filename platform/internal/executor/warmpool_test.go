package executor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	runnerv1 "github.com/rat-data/rat/platform/gen/runner/v1"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock runner client ---

type mockRunnerClient struct {
	submitFunc    func(ctx context.Context, req *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error)
	getStatusFunc func(ctx context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error)
	cancelFunc    func(ctx context.Context, req *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error)
	previewFunc   func(req *connect.Request[runnerv1.PreviewPipelineRequest]) (*connect.Response[runnerv1.PreviewPipelineResponse], error)
	validateFunc  func(ctx context.Context, req *connect.Request[runnerv1.ValidatePipelineRequest]) (*connect.Response[runnerv1.ValidatePipelineResponse], error)
}

func (m *mockRunnerClient) SubmitPipeline(ctx context.Context, req *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
	if m.submitFunc != nil {
		return m.submitFunc(ctx, req)
	}
	return connect.NewResponse(&runnerv1.SubmitPipelineResponse{}), nil
}

func (m *mockRunnerClient) GetRunStatus(ctx context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
	if m.getStatusFunc != nil {
		return m.getStatusFunc(ctx, req)
	}
	return connect.NewResponse(&commonv1.GetRunStatusResponse{Status: commonv1.RunStatus_RUN_STATUS_RUNNING}), nil
}

func (m *mockRunnerClient) StreamLogs(ctx context.Context, req *connect.Request[commonv1.StreamLogsRequest]) (*connect.ServerStreamForClient[commonv1.LogEntry], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (m *mockRunnerClient) CancelRun(ctx context.Context, req *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error) {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, req)
	}
	return connect.NewResponse(&commonv1.CancelRunResponse{Cancelled: true}), nil
}

func (m *mockRunnerClient) PreviewPipeline(_ context.Context, req *connect.Request[runnerv1.PreviewPipelineRequest]) (*connect.Response[runnerv1.PreviewPipelineResponse], error) {
	if m.previewFunc != nil {
		return m.previewFunc(req)
	}
	return connect.NewResponse(&runnerv1.PreviewPipelineResponse{}), nil
}

func (m *mockRunnerClient) ValidatePipeline(ctx context.Context, req *connect.Request[runnerv1.ValidatePipelineRequest]) (*connect.Response[runnerv1.ValidatePipelineResponse], error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, req)
	}
	return connect.NewResponse(&runnerv1.ValidatePipelineResponse{Valid: true}), nil
}

// --- Mock run store ---

type mockRunStore struct {
	mu   sync.Mutex
	runs map[string]domain.RunStatus
	errs map[string]*string
}

func newMockRunStore() *mockRunStore {
	return &mockRunStore{
		runs: make(map[string]domain.RunStatus),
		errs: make(map[string]*string),
	}
}

func (m *mockRunStore) ListRuns(_ context.Context, _ api.RunFilter) ([]domain.Run, error) {
	return nil, nil
}

func (m *mockRunStore) CountRuns(_ context.Context, _ api.RunFilter) (int, error) {
	return 0, nil
}

func (m *mockRunStore) GetRun(_ context.Context, runID string) (*domain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status, ok := m.runs[runID]
	if !ok {
		return nil, nil
	}
	id, _ := uuid.Parse(runID)
	return &domain.Run{ID: id, Status: status}, nil
}

func (m *mockRunStore) CreateRun(_ context.Context, run *domain.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	run.ID = uuid.New()
	m.runs[run.ID.String()] = run.Status
	return nil
}

func (m *mockRunStore) UpdateRunStatus(_ context.Context, runID string, status domain.RunStatus, errMsg *string, _, _ *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[runID] = status
	m.errs[runID] = errMsg
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

func (m *mockRunStore) getStatus(runID string) domain.RunStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[runID]
}

func (m *mockRunStore) getError(runID string) *string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.errs[runID]
}

// --- Helpers ---

func testRun() *domain.Run {
	return &domain.Run{
		ID:     uuid.New(),
		Status: domain.RunStatusPending,
	}
}

func testPipeline() *domain.Pipeline {
	return &domain.Pipeline{
		ID:        uuid.New(),
		Namespace: "default",
		Layer:     domain.LayerSilver,
		Name:      "orders",
		Type:      "sql",
	}
}

// --- Tests ---

func TestSubmit_RunnerAvailable_UpdatesToRunning(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	run := testRun()
	pipeline := testPipeline()

	err := exec.Submit(context.Background(), run, pipeline)
	require.NoError(t, err)

	assert.Equal(t, domain.RunStatusRunning, store.getStatus(run.ID.String()))

	exec.mu.Lock()
	_, tracked := exec.active[run.ID.String()]
	exec.mu.Unlock()
	assert.True(t, tracked)
}

func TestSubmit_RunnerUnavailable_UpdatesToFailed(t *testing.T) {
	mock := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	run := testRun()
	pipeline := testPipeline()

	err := exec.Submit(context.Background(), run, pipeline)
	assert.Error(t, err)

	assert.Equal(t, domain.RunStatusFailed, store.getStatus(run.ID.String()))
	assert.NotNil(t, store.getError(run.ID.String()))
}

func TestSubmit_BuildsCorrectRequest(t *testing.T) {
	var captured *runnerv1.SubmitPipelineRequest
	mock := &mockRunnerClient{
		submitFunc: func(_ context.Context, req *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			captured = req.Msg
			return connect.NewResponse(&runnerv1.SubmitPipelineResponse{}), nil
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	run := &domain.Run{ID: uuid.New(), Status: domain.RunStatusPending, Trigger: "schedule:hourly"}
	pipeline := &domain.Pipeline{
		Namespace: "analytics",
		Layer:     domain.LayerGold,
		Name:      "revenue",
	}

	err := exec.Submit(context.Background(), run, pipeline)
	require.NoError(t, err)

	require.NotNil(t, captured)
	assert.Equal(t, "analytics", captured.Namespace)
	assert.Equal(t, commonv1.Layer_LAYER_GOLD, captured.Layer)
	assert.Equal(t, "revenue", captured.PipelineName)
	assert.Equal(t, "schedule:hourly", captured.Trigger)
}

func TestPoll_RunCompletes_UpdatesDB(t *testing.T) {
	runID := uuid.New().String()

	mock := &mockRunnerClient{
		getStatusFunc: func(_ context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
			return connect.NewResponse(&commonv1.GetRunStatusResponse{
				RunId:  req.Msg.RunId,
				Status: commonv1.RunStatus_RUN_STATUS_SUCCESS,
			}), nil
		},
	}
	store := newMockRunStore()
	store.runs[runID] = domain.RunStatusRunning

	exec := newWarmPoolExecutorWithClient(mock, store)
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}

	exec.poll(context.Background())

	assert.Equal(t, domain.RunStatusSuccess, store.getStatus(runID))

	exec.mu.Lock()
	_, tracked := exec.active[runID]
	exec.mu.Unlock()
	assert.False(t, tracked)
}

func TestPoll_RunFails_UpdatesDBWithError(t *testing.T) {
	runID := uuid.New().String()

	mock := &mockRunnerClient{
		getStatusFunc: func(_ context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
			return connect.NewResponse(&commonv1.GetRunStatusResponse{
				RunId:  req.Msg.RunId,
				Status: commonv1.RunStatus_RUN_STATUS_FAILED,
				Error:  "DuckDB OOM",
			}), nil
		},
	}
	store := newMockRunStore()
	store.runs[runID] = domain.RunStatusRunning

	exec := newWarmPoolExecutorWithClient(mock, store)
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}

	exec.poll(context.Background())

	assert.Equal(t, domain.RunStatusFailed, store.getStatus(runID))
	errMsg := store.getError(runID)
	require.NotNil(t, errMsg)
	assert.Equal(t, "DuckDB OOM", *errMsg)
}

func TestCancel_RunningRun_UpdatesDB(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	runID := uuid.New().String()
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}

	err := exec.Cancel(context.Background(), runID)
	require.NoError(t, err)

	exec.mu.Lock()
	_, tracked := exec.active[runID]
	exec.mu.Unlock()
	assert.False(t, tracked)
}

func TestCancel_RunnerUnavailable_ReturnsError(t *testing.T) {
	mock := &mockRunnerClient{
		cancelFunc: func(_ context.Context, _ *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	err := exec.Cancel(context.Background(), uuid.New().String())
	assert.Error(t, err)
}

func TestPreview_ForwardsInlineCode(t *testing.T) {
	var captured *runnerv1.PreviewPipelineRequest
	mock := &mockRunnerClient{
		previewFunc: func(req *connect.Request[runnerv1.PreviewPipelineRequest]) (*connect.Response[runnerv1.PreviewPipelineResponse], error) {
			captured = req.Msg
			return connect.NewResponse(&runnerv1.PreviewPipelineResponse{}), nil
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	pipeline := testPipeline()
	_, err := exec.Preview(context.Background(), pipeline, 100, nil, "SELECT 1 AS x")
	require.NoError(t, err)

	require.NotNil(t, captured)
	assert.Equal(t, "SELECT 1 AS x", captured.Code)
	assert.Equal(t, "sql", captured.PipelineType)
}

func TestPreview_EmptyCodeNotForwarded(t *testing.T) {
	var captured *runnerv1.PreviewPipelineRequest
	mock := &mockRunnerClient{
		previewFunc: func(req *connect.Request[runnerv1.PreviewPipelineRequest]) (*connect.Response[runnerv1.PreviewPipelineResponse], error) {
			captured = req.Msg
			return connect.NewResponse(&runnerv1.PreviewPipelineResponse{}), nil
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	pipeline := testPipeline()
	_, err := exec.Preview(context.Background(), pipeline, 100, nil, "")
	require.NoError(t, err)

	require.NotNil(t, captured)
	assert.Empty(t, captured.Code)
}

func TestSubmit_ResourceExhausted_ReturnsErrRunnerBusy(t *testing.T) {
	mock := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("10/10 concurrent runs"))
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	run := testRun()
	pipeline := testPipeline()

	err := exec.Submit(context.Background(), run, pipeline)
	assert.Error(t, err)

	// Should wrap ErrRunnerBusy
	assert.ErrorIs(t, err, ErrRunnerBusy)

	// Run should NOT be marked as failed — it stays in pending for retry
	status := store.getStatus(run.ID.String())
	assert.NotEqual(t, domain.RunStatusFailed, status, "run should NOT be marked failed on RESOURCE_EXHAUSTED")
}

func TestSubmit_ResourceExhausted_DoesNotTrackAsActive(t *testing.T) {
	mock := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("busy"))
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	run := testRun()
	pipeline := testPipeline()

	_ = exec.Submit(context.Background(), run, pipeline)

	// Should not be tracked in active map
	exec.mu.Lock()
	_, tracked := exec.active[run.ID.String()]
	exec.mu.Unlock()
	assert.False(t, tracked, "resource-exhausted run should not be tracked as active")
}

func TestStartStop_BackgroundPollRuns(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)
	exec.pollInterval = 10 * time.Millisecond

	ctx := context.Background()
	exec.Start(ctx)

	// Let it tick a few times
	time.Sleep(50 * time.Millisecond)

	exec.Stop()
	// Should not hang — goroutine exited
}

// --- Status Callback Tests ---

func TestCallback_SuccessUpdatesDB(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	runID := uuid.New().String()
	store.runs[runID] = domain.RunStatusRunning
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}
	exec.runnerIDs[runID] = runID

	update := api.RunStatusUpdate{
		RunID:       runID,
		Status:      "success",
		DurationMs:  5000,
		RowsWritten: 100,
	}

	err := exec.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err)

	assert.Equal(t, domain.RunStatusSuccess, store.getStatus(runID))

	// Should be removed from active map
	exec.mu.Lock()
	_, tracked := exec.active[runID]
	exec.mu.Unlock()
	assert.False(t, tracked, "run should be removed from active map after callback")
}

func TestCallback_FailedUpdatesDBWithError(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	runID := uuid.New().String()
	store.runs[runID] = domain.RunStatusRunning
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}
	exec.runnerIDs[runID] = runID

	update := api.RunStatusUpdate{
		RunID:       runID,
		Status:      "failed",
		Error:       "DuckDB OOM",
		DurationMs:  3000,
		RowsWritten: 0,
	}

	err := exec.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err)

	assert.Equal(t, domain.RunStatusFailed, store.getStatus(runID))
	errMsg := store.getError(runID)
	require.NotNil(t, errMsg)
	assert.Equal(t, "DuckDB OOM", *errMsg)
}

func TestCallback_UnknownRunAcceptedIdempotently(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	// Run not in active map — should accept silently
	update := api.RunStatusUpdate{
		RunID:  "nonexistent-run",
		Status: "success",
	}

	err := exec.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err, "unknown run should be accepted idempotently")
}

func TestCallback_FiresOnRunComplete(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	runID := uuid.New().String()
	store.runs[runID] = domain.RunStatusRunning
	trackedRun := &domain.Run{Status: domain.RunStatusRunning}
	exec.active[runID] = trackedRun
	exec.runnerIDs[runID] = runID

	// Set up OnRunComplete callback
	var callbackRun *domain.Run
	var callbackStatus domain.RunStatus
	done := make(chan struct{})
	exec.OnRunComplete = func(_ context.Context, run *domain.Run, status domain.RunStatus) {
		callbackRun = run
		callbackStatus = status
		close(done)
	}

	update := api.RunStatusUpdate{
		RunID:  runID,
		Status: "success",
	}

	err := exec.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err)

	// Wait for async callback
	select {
	case <-done:
		assert.Equal(t, trackedRun, callbackRun)
		assert.Equal(t, domain.RunStatusSuccess, callbackStatus)
	case <-time.After(2 * time.Second):
		t.Fatal("OnRunComplete callback not fired within timeout")
	}
}

func TestCallback_CleansUpArchivedZones(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	runID := uuid.New().String()
	store.runs[runID] = domain.RunStatusRunning
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}
	exec.runnerIDs[runID] = runID

	// Set up a mock landing zone store
	lz := &mockLandingZoneStore{}
	exec.LandingZones = lz

	update := api.RunStatusUpdate{
		RunID:                runID,
		Status:               "success",
		ArchivedLandingZones: []string{"default/raw-uploads"},
	}

	err := exec.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err)

	// Landing zone cleanup should have been attempted
	assert.True(t, lz.getZoneCalled, "cleanupArchivedZones should call GetZone")
}

func TestFallbackPollInterval_Is60Seconds(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	assert.Equal(t, FallbackPollInterval, exec.pollInterval,
		"default poll interval should be 60s (fallback for push-based callbacks)")
}

// --- Mock landing zone store for callback tests ---

type mockLandingZoneStore struct {
	getZoneCalled bool
}

func (m *mockLandingZoneStore) ListZones(_ context.Context, _ api.LandingZoneFilter) ([]api.LandingZoneListItem, error) {
	return nil, nil
}

func (m *mockLandingZoneStore) GetZone(_ context.Context, _, _ string) (*api.LandingZoneDetail, error) {
	m.getZoneCalled = true
	return nil, nil // zone not found — that's fine for test
}

func (m *mockLandingZoneStore) CreateZone(_ context.Context, _ *domain.LandingZone) error {
	return nil
}

func (m *mockLandingZoneStore) DeleteZone(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockLandingZoneStore) UpdateZone(_ context.Context, _, _ string, _, _, _ *string) (*domain.LandingZone, error) {
	return nil, nil
}

func (m *mockLandingZoneStore) ListFiles(_ context.Context, _ uuid.UUID) ([]domain.LandingFile, error) {
	return nil, nil
}

func (m *mockLandingZoneStore) CreateFile(_ context.Context, _ *domain.LandingFile) error {
	return nil
}

func (m *mockLandingZoneStore) GetFile(_ context.Context, _ uuid.UUID) (*domain.LandingFile, error) {
	return nil, nil
}

func (m *mockLandingZoneStore) DeleteFile(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockLandingZoneStore) GetZoneByID(_ context.Context, _ uuid.UUID) (*domain.LandingZone, error) {
	return nil, nil
}

func (m *mockLandingZoneStore) UpdateZoneLifecycle(_ context.Context, _ uuid.UUID, _ *int, _ *bool) error {
	return nil
}

func (m *mockLandingZoneStore) ListZonesWithAutoPurge(_ context.Context) ([]domain.LandingZone, error) {
	return nil, nil
}
