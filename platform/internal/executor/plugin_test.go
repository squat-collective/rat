package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	executorv1 "github.com/rat-data/rat/platform/gen/executor/v1"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock executor client ---

type mockExecutorClient struct {
	submitFunc    func(ctx context.Context, req *connect.Request[executorv1.SubmitRequest]) (*connect.Response[executorv1.SubmitResponse], error)
	getStatusFunc func(ctx context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error)
	cancelFunc    func(ctx context.Context, req *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error)
}

func (m *mockExecutorClient) Submit(ctx context.Context, req *connect.Request[executorv1.SubmitRequest]) (*connect.Response[executorv1.SubmitResponse], error) {
	if m.submitFunc != nil {
		return m.submitFunc(ctx, req)
	}
	return connect.NewResponse(&executorv1.SubmitResponse{}), nil
}

func (m *mockExecutorClient) GetRunStatus(ctx context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
	if m.getStatusFunc != nil {
		return m.getStatusFunc(ctx, req)
	}
	return connect.NewResponse(&commonv1.GetRunStatusResponse{Status: commonv1.RunStatus_RUN_STATUS_RUNNING}), nil
}

func (m *mockExecutorClient) StreamLogs(_ context.Context, _ *connect.Request[commonv1.StreamLogsRequest]) (*connect.ServerStreamForClient[commonv1.LogEntry], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (m *mockExecutorClient) Cancel(ctx context.Context, req *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error) {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, req)
	}
	return connect.NewResponse(&commonv1.CancelRunResponse{Cancelled: true}), nil
}

// --- Tests ---

func TestPluginSubmit_Available_UpdatesToRunning(t *testing.T) {
	mock := &mockExecutorClient{}
	store := newMockRunStore()
	exec := newPluginExecutorWithClient(mock, store)

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

func TestPluginSubmit_Unavailable_UpdatesToFailed(t *testing.T) {
	mock := &mockExecutorClient{
		submitFunc: func(_ context.Context, _ *connect.Request[executorv1.SubmitRequest]) (*connect.Response[executorv1.SubmitResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))
		},
	}
	store := newMockRunStore()
	exec := newPluginExecutorWithClient(mock, store)

	run := testRun()
	pipeline := testPipeline()

	err := exec.Submit(context.Background(), run, pipeline)
	assert.Error(t, err)

	assert.Equal(t, domain.RunStatusFailed, store.getStatus(run.ID.String()))
	assert.NotNil(t, store.getError(run.ID.String()))
}

func TestPluginSubmit_BuildsCorrectRequest(t *testing.T) {
	var captured *executorv1.SubmitRequest
	mock := &mockExecutorClient{
		submitFunc: func(_ context.Context, req *connect.Request[executorv1.SubmitRequest]) (*connect.Response[executorv1.SubmitResponse], error) {
			captured = req.Msg
			return connect.NewResponse(&executorv1.SubmitResponse{}), nil
		},
	}
	store := newMockRunStore()
	exec := newPluginExecutorWithClient(mock, store)

	runID := uuid.New()
	run := &domain.Run{ID: runID, Status: domain.RunStatusPending, Trigger: "schedule:hourly"}
	pipeline := &domain.Pipeline{
		Namespace: "analytics",
		Layer:     domain.LayerGold,
		Name:      "revenue",
	}

	err := exec.Submit(context.Background(), run, pipeline)
	require.NoError(t, err)

	require.NotNil(t, captured)
	assert.Equal(t, runID.String(), captured.RunId)
	assert.Equal(t, "analytics", captured.Namespace)
	assert.Equal(t, commonv1.Layer_LAYER_GOLD, captured.Layer)
	assert.Equal(t, "revenue", captured.PipelineName)
	assert.Equal(t, "schedule:hourly", captured.Trigger)
}

func TestPluginPoll_RunCompletes_UpdatesDB(t *testing.T) {
	runID := uuid.New().String()

	mock := &mockExecutorClient{
		getStatusFunc: func(_ context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
			return connect.NewResponse(&commonv1.GetRunStatusResponse{
				RunId:       req.Msg.RunId,
				Status:      commonv1.RunStatus_RUN_STATUS_SUCCESS,
				RowsWritten: 100,
				DurationMs:  2500,
			}), nil
		},
	}
	store := newMockRunStore()
	store.runs[runID] = domain.RunStatusRunning

	exec := newPluginExecutorWithClient(mock, store)
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}

	exec.poll(context.Background())

	assert.Equal(t, domain.RunStatusSuccess, store.getStatus(runID))

	exec.mu.Lock()
	_, tracked := exec.active[runID]
	exec.mu.Unlock()
	assert.False(t, tracked)
}

func TestPluginPoll_RunFails_UpdatesDBWithError(t *testing.T) {
	runID := uuid.New().String()

	mock := &mockExecutorClient{
		getStatusFunc: func(_ context.Context, req *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
			return connect.NewResponse(&commonv1.GetRunStatusResponse{
				RunId:  req.Msg.RunId,
				Status: commonv1.RunStatus_RUN_STATUS_FAILED,
				Error:  "container exit code 1",
			}), nil
		},
	}
	store := newMockRunStore()
	store.runs[runID] = domain.RunStatusRunning

	exec := newPluginExecutorWithClient(mock, store)
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}

	exec.poll(context.Background())

	assert.Equal(t, domain.RunStatusFailed, store.getStatus(runID))
	errMsg := store.getError(runID)
	require.NotNil(t, errMsg)
	assert.Equal(t, "container exit code 1", *errMsg)
}

func TestPluginPoll_StillRunning_NoDBUpdate(t *testing.T) {
	runID := uuid.New().String()

	mock := &mockExecutorClient{
		getStatusFunc: func(_ context.Context, _ *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
			return connect.NewResponse(&commonv1.GetRunStatusResponse{
				Status: commonv1.RunStatus_RUN_STATUS_RUNNING,
			}), nil
		},
	}
	store := newMockRunStore()
	store.runs[runID] = domain.RunStatusRunning

	exec := newPluginExecutorWithClient(mock, store)
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}

	exec.poll(context.Background())

	// Still running — no change
	assert.Equal(t, domain.RunStatusRunning, store.getStatus(runID))

	exec.mu.Lock()
	_, tracked := exec.active[runID]
	exec.mu.Unlock()
	assert.True(t, tracked, "run should still be tracked while running")
}

func TestPluginCancel_RemovesFromActive(t *testing.T) {
	mock := &mockExecutorClient{}
	store := newMockRunStore()
	exec := newPluginExecutorWithClient(mock, store)

	runID := uuid.New().String()
	exec.active[runID] = &domain.Run{Status: domain.RunStatusRunning}

	err := exec.Cancel(context.Background(), runID)
	require.NoError(t, err)

	exec.mu.Lock()
	_, tracked := exec.active[runID]
	exec.mu.Unlock()
	assert.False(t, tracked)
}

func TestPluginStartStop_BackgroundPollRuns(t *testing.T) {
	mock := &mockExecutorClient{}
	store := newMockRunStore()
	exec := newPluginExecutorWithClient(mock, store)
	exec.pollInterval = 10 * time.Millisecond

	ctx := context.Background()
	exec.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	exec.Stop()
	// Should not hang — goroutine exited
}
