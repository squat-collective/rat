package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	connect "connectrpc.com/connect"
	commonv1 "github.com/rat-data/rat/platform/gen/common/v1"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureSlog redirects slog.Default() to a JSON handler writing to a buffer
// for the duration of the test. Returns the buffer and a restore func.
//
// Used to assert that nested log statements inside poll() / HandleStatusCallback
// inherit the run_id (and pipeline_id when known) we bound at the top of each
// scope — that's the whole point of the observability commit: a single grep on
// run_id should surface every log line about that run.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	prev := slog.Default()
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return &buf, func() { slog.SetDefault(prev) }
}

// parseSlogLines parses each newline-delimited JSON object emitted by slog.
func parseSlogLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "line is not JSON: %s", line)
		out = append(out, m)
	}
	return out
}

func TestPoll_BindsRunIDToEveryNestedLog(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	runID := uuid.New().String()
	pipelineID := uuid.New()

	// Runner returns NotFound so we hit the "run not found (will retry)" log
	// branch — that branch lives inside the per-run loop, so any log it emits
	// must include the run_id we bound at the top.
	mock := &mockRunnerClient{
		getStatusFunc: func(_ context.Context, _ *connect.Request[commonv1.GetRunStatusRequest]) (*connect.Response[commonv1.GetRunStatusResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("gone"))
		},
	}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)
	exec.active[runID] = &domain.Run{PipelineID: pipelineID, Status: domain.RunStatusRunning}
	exec.runnerIDs[runID] = "runner-" + runID

	exec.poll(context.Background())

	lines := parseSlogLines(t, buf.String())
	require.NotEmpty(t, lines, "poll should have emitted at least one log line")

	// Every log line emitted during the iteration must carry run_id +
	// pipeline_id since we bound them at the top of the scope.
	for _, line := range lines {
		assert.Equal(t, runID, line["run_id"], "every poll log should carry run_id")
		assert.Equal(t, pipelineID.String(), line["pipeline_id"], "every poll log should carry pipeline_id")
		assert.Equal(t, "runner-"+runID, line["runner_id"], "every poll log should carry runner_id")
	}
}

func TestPoll_RunCompletion_LogsCarryRunID(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	runID := uuid.New().String()
	pipelineID := uuid.New()

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
	exec.active[runID] = &domain.Run{PipelineID: pipelineID, Status: domain.RunStatusRunning}

	exec.poll(context.Background())

	lines := parseSlogLines(t, buf.String())
	// Find the "poll: run completed" line and assert run_id is on it.
	var found bool
	for _, line := range lines {
		if line["msg"] == "poll: run completed" {
			found = true
			assert.Equal(t, runID, line["run_id"], "completion log must carry run_id")
			assert.Equal(t, pipelineID.String(), line["pipeline_id"], "completion log must carry pipeline_id")
		}
	}
	assert.True(t, found, "expected a 'poll: run completed' log line")
}

func TestHandleStatusCallback_LogsCarryRunID(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	runID := uuid.New().String()
	pipelineID := uuid.New()

	mock := &mockRunnerClient{}
	store := newMockRunStore()
	store.runs[runID] = domain.RunStatusRunning
	exec := newWarmPoolExecutorWithClient(mock, store)
	exec.active[runID] = &domain.Run{PipelineID: pipelineID, Status: domain.RunStatusRunning}
	exec.runnerIDs[runID] = runID

	update := api.RunStatusUpdate{
		RunID:  runID,
		Status: "success",
	}

	err := exec.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err)

	lines := parseSlogLines(t, buf.String())
	var found bool
	for _, line := range lines {
		if line["msg"] == "callback: run completed" {
			found = true
			assert.Equal(t, runID, line["run_id"], "callback log must carry run_id")
			assert.Equal(t, pipelineID.String(), line["pipeline_id"], "callback log must carry pipeline_id")
		}
	}
	assert.True(t, found, "expected a 'callback: run completed' log line")
}

func TestHandleStatusCallback_UnknownRun_LogsRunID(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	mock := &mockRunnerClient{}
	store := newMockRunStore()
	exec := newWarmPoolExecutorWithClient(mock, store)

	update := api.RunStatusUpdate{
		RunID:  "unknown-run",
		Status: "success",
	}

	err := exec.HandleStatusCallback(context.Background(), update)
	require.NoError(t, err)

	lines := parseSlogLines(t, buf.String())
	require.NotEmpty(t, lines)
	// Even the "already processed" log line must carry run_id.
	for _, line := range lines {
		assert.Equal(t, "unknown-run", line["run_id"], "every callback log should carry run_id")
	}
}
