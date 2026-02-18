package executor

import (
	"context"
	"errors"
	"testing"

	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	runnerv1 "github.com/rat-data/rat/platform/gen/runner/v1"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helpers for round-robin tests ---

func newTestRRExecutor(clients ...*mockRunnerClient) (*RoundRobinExecutor, *mockRunStore) {
	store := newMockRunStore()
	executors := make([]*WarmPoolExecutor, len(clients))
	for i, client := range clients {
		executors[i] = newWarmPoolExecutorWithClient(client, store)
	}
	return newRoundRobinExecutorFromPool(executors), store
}

// --- Round-robin selection tests ---

func TestRoundRobin_DistributesAcrossRunners(t *testing.T) {
	// Track which runner received each submission
	calls := make([]int, 0)
	makeClient := func(idx int) *mockRunnerClient {
		return &mockRunnerClient{
			submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
				calls = append(calls, idx)
				return connect.NewResponse(&runnerv1.SubmitPipelineResponse{RunId: "r"}), nil
			},
		}
	}

	rr, _ := newTestRRExecutor(makeClient(0), makeClient(1), makeClient(2))

	for i := 0; i < 6; i++ {
		run := testRun()
		pipeline := testPipeline()
		err := rr.Submit(context.Background(), run, pipeline)
		require.NoError(t, err)
	}

	// Should round-robin: 0, 1, 2, 0, 1, 2
	assert.Equal(t, []int{0, 1, 2, 0, 1, 2}, calls)
}

// --- RESOURCE_EXHAUSTED failover tests ---

func TestRoundRobin_FailoverOnResourceExhausted(t *testing.T) {
	// Runner 0 returns RESOURCE_EXHAUSTED, runner 1 succeeds
	busy := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("at capacity"))
		},
	}
	healthy := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			return connect.NewResponse(&runnerv1.SubmitPipelineResponse{RunId: "ok"}), nil
		},
	}

	rr, store := newTestRRExecutor(busy, healthy)

	run := testRun()
	pipeline := testPipeline()
	err := rr.Submit(context.Background(), run, pipeline)
	require.NoError(t, err)

	// Run should be marked as running (via healthy runner)
	assert.Equal(t, domain.RunStatusRunning, store.getStatus(run.ID.String()))
}

func TestRoundRobin_AllExhausted_ReturnsErrRunnerBusy(t *testing.T) {
	busy := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("full"))
		},
	}

	rr, store := newTestRRExecutor(busy, busy, busy)

	run := testRun()
	pipeline := testPipeline()
	err := rr.Submit(context.Background(), run, pipeline)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrRunnerBusy)

	// Run should NOT be marked as failed (stays pending for retry)
	status := store.getStatus(run.ID.String())
	assert.NotEqual(t, domain.RunStatusFailed, status)
}

func TestRoundRobin_NonCapacityError_ReturnsImmediately(t *testing.T) {
	// Runner 0 returns a non-capacity error (e.g., unavailable)
	// Runner 1 should NOT be tried
	var runner1Called bool
	broken := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))
		},
	}
	healthy := &mockRunnerClient{
		submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
			runner1Called = true
			return connect.NewResponse(&runnerv1.SubmitPipelineResponse{RunId: "ok"}), nil
		},
	}

	rr, store := newTestRRExecutor(broken, healthy)

	run := testRun()
	pipeline := testPipeline()
	err := rr.Submit(context.Background(), run, pipeline)

	assert.Error(t, err)
	assert.False(t, runner1Called, "should not try next runner on non-capacity errors")

	// Run should be marked as failed (non-capacity error)
	assert.Equal(t, domain.RunStatusFailed, store.getStatus(run.ID.String()))
}

func TestRoundRobin_FailoverWrapsAround(t *testing.T) {
	// 3 runners: start at runner 2 (via counter manipulation), runner 2 is busy,
	// wraps around to runner 0 which succeeds
	calls := make([]int, 0)
	makeClient := func(idx int, exhausted bool) *mockRunnerClient {
		return &mockRunnerClient{
			submitFunc: func(_ context.Context, _ *connect.Request[runnerv1.SubmitPipelineRequest]) (*connect.Response[runnerv1.SubmitPipelineResponse], error) {
				calls = append(calls, idx)
				if exhausted {
					return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("full"))
				}
				return connect.NewResponse(&runnerv1.SubmitPipelineResponse{RunId: "ok"}), nil
			},
		}
	}

	rr, _ := newTestRRExecutor(
		makeClient(0, false), // healthy
		makeClient(1, true),  // busy
		makeClient(2, true),  // busy
	)

	// Advance counter so next() returns 1 (second runner)
	rr.counter.Store(1)

	run := testRun()
	pipeline := testPipeline()
	err := rr.Submit(context.Background(), run, pipeline)
	require.NoError(t, err)

	// Should try: 1 (busy) -> 2 (busy) -> 0 (ok)
	assert.Equal(t, []int{1, 2, 0}, calls)
}

// --- Cancel/GetLogs fan-out tests ---

func TestRoundRobin_Cancel_TriesAllUntilSuccess(t *testing.T) {
	notFound := &mockRunnerClient{
		cancelFunc: func(_ context.Context, _ *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not here"))
		},
	}
	found := &mockRunnerClient{
		cancelFunc: func(_ context.Context, _ *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error) {
			return connect.NewResponse(&commonv1.CancelRunResponse{Cancelled: true}), nil
		},
	}

	rr, _ := newTestRRExecutor(notFound, found)
	err := rr.Cancel(context.Background(), "some-run-id")
	assert.NoError(t, err)
}

func TestRoundRobin_Cancel_ReturnsLastErrorIfAllFail(t *testing.T) {
	notFound := &mockRunnerClient{
		cancelFunc: func(_ context.Context, _ *connect.Request[commonv1.CancelRunRequest]) (*connect.Response[commonv1.CancelRunResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not here"))
		},
	}

	rr, _ := newTestRRExecutor(notFound, notFound)
	err := rr.Cancel(context.Background(), "some-run-id")
	assert.Error(t, err)
}

// --- ParseRunnerAddrs tests ---

func TestParseRunnerAddrs_SingleAddr(t *testing.T) {
	addrs := ParseRunnerAddrs("http://runner:50051")
	assert.Equal(t, []string{"http://runner:50051"}, addrs)
}

func TestParseRunnerAddrs_MultipleAddrs(t *testing.T) {
	addrs := ParseRunnerAddrs("http://runner-1:50051,http://runner-2:50051,http://runner-3:50051")
	assert.Equal(t, []string{
		"http://runner-1:50051",
		"http://runner-2:50051",
		"http://runner-3:50051",
	}, addrs)
}

func TestParseRunnerAddrs_TrimsWhitespace(t *testing.T) {
	addrs := ParseRunnerAddrs("  http://runner-1:50051 , http://runner-2:50051  ")
	assert.Equal(t, []string{
		"http://runner-1:50051",
		"http://runner-2:50051",
	}, addrs)
}

func TestParseRunnerAddrs_EmptyString(t *testing.T) {
	addrs := ParseRunnerAddrs("")
	assert.Nil(t, addrs)
}

func TestParseRunnerAddrs_SkipsEmptyEntries(t *testing.T) {
	addrs := ParseRunnerAddrs("http://runner-1:50051,,http://runner-2:50051,")
	assert.Equal(t, []string{
		"http://runner-1:50051",
		"http://runner-2:50051",
	}, addrs)
}

// --- GetLogs fan-out ---

func TestRoundRobin_GetLogs_TriesAllUntilSuccess(t *testing.T) {
	// We can't easily test GetLogs since it depends on runnerIDs tracking,
	// but we verify the fan-out mechanism works for the error path
	store := newMockRunStore()

	// Two executors, both will fail since no runs are tracked
	exec1 := newWarmPoolExecutorWithClient(&mockRunnerClient{}, store)
	exec2 := newWarmPoolExecutorWithClient(&mockRunnerClient{}, store)

	rr := newRoundRobinExecutorFromPool([]*WarmPoolExecutor{exec1, exec2})

	_, err := rr.GetLogs(context.Background(), "unknown-run")
	assert.Error(t, err, "should return error when no executor tracks the run")
}

// --- Poll status distribution ---

func TestRoundRobin_Poll_EachExecutorPollsItsOwnRuns(t *testing.T) {
	// Runner 0 has an active run that completes
	mock0 := &mockRunnerClient{
		getStatusFunc: func(_ context.Context, _ *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
			return connect.NewResponse(&commonv1.GetRunStatusResponse{
				Status: commonv1.RunStatus_RUN_STATUS_SUCCESS,
			}), nil
		},
	}
	// Runner 1 has no active runs
	mock1 := &mockRunnerClient{}

	store := newMockRunStore()
	exec0 := newWarmPoolExecutorWithClient(mock0, store)
	exec1 := newWarmPoolExecutorWithClient(mock1, store)

	run := testRun()
	runID := run.ID.String()
	store.runs[runID] = domain.RunStatusRunning

	// Track the run in executor 0 only
	exec0.active[runID] = run
	exec0.runnerIDs[runID] = runID

	_ = newRoundRobinExecutorFromPool([]*WarmPoolExecutor{exec0, exec1})

	// Poll should update the run via executor 0
	exec0.poll(context.Background())

	assert.Equal(t, domain.RunStatusSuccess, store.getStatus(runID))
}

// --- Start/Stop lifecycle ---

func TestRoundRobin_StartStop(t *testing.T) {
	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec1 := newWarmPoolExecutorWithClient(mock, store)
	exec2 := newWarmPoolExecutorWithClient(mock, store)

	rr := newRoundRobinExecutorFromPool([]*WarmPoolExecutor{exec1, exec2})

	ctx := context.Background()
	rr.Start(ctx)
	rr.Stop() // Should not hang
}
