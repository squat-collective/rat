package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// RunStore implements api.RunStore backed by Postgres.
type RunStore struct {
	pool     *pgxpool.Pool
	q        *gen.Queries
	EventBus EventBus // optional — publishes run_completed events when set
}

// NewRunStore creates a RunStore backed by the given pool.
func NewRunStore(pool *pgxpool.Pool) *RunStore {
	return &RunStore{pool: pool, q: gen.New(pool)}
}

// runListColumns is the column list for run list queries.
const runListColumns = `r.id, r.pipeline_id, r.status, r.trigger, r.started_at, r.finished_at,
       r.duration_ms, r.rows_written, r.error, r.logs_s3_path, r.created_at`

// runWhereClause builds the shared WHERE clause and args for run list/count queries.
func runWhereClause(filter api.RunFilter) (string, []interface{}, int) {
	where := ` WHERE 1=1`
	args := []interface{}{}
	argN := 1

	if filter.Namespace != "" {
		where += fmt.Sprintf(" AND p.namespace = $%d", argN)
		args = append(args, filter.Namespace)
		argN++
	}
	if filter.Layer != "" {
		where += fmt.Sprintf(" AND p.layer = $%d", argN)
		args = append(args, filter.Layer)
		argN++
	}
	if filter.Pipeline != "" {
		where += fmt.Sprintf(" AND p.name = $%d", argN)
		args = append(args, filter.Pipeline)
		argN++
	}
	if filter.PipelineID != "" {
		where += fmt.Sprintf(" AND r.pipeline_id = $%d", argN)
		args = append(args, filter.PipelineID)
		argN++
	}
	if filter.Status != "" {
		where += fmt.Sprintf(" AND r.status = $%d", argN)
		args = append(args, filter.Status)
		argN++
	}
	return where, args, argN
}

func (s *RunStore) ListRuns(ctx context.Context, filter api.RunFilter) ([]domain.Run, error) {
	where, args, argN := runWhereClause(filter)
	query := `SELECT ` + runListColumns + ` FROM runs r JOIN pipelines p ON r.pipeline_id = p.id` + where + ` ORDER BY r.created_at DESC`

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argN, argN+1)
		args = append(args, filter.Limit, filter.Offset)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var result []domain.Run
	for rows.Next() {
		var (
			id, pipelineID        uuid.UUID
			status, trigger       string
			startedAt, finishedAt *time.Time
			durationMs            pgtype.Int4
			rowsWritten           pgtype.Int8
			errText               pgtype.Text
			logsS3Path            pgtype.Text
			createdAt             time.Time
		)
		if err := rows.Scan(&id, &pipelineID, &status, &trigger,
			&startedAt, &finishedAt, &durationMs, &rowsWritten,
			&errText, &logsS3Path, &createdAt); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		result = append(result, runRowToDomain(gen.Run{
			ID: id, PipelineID: pipelineID,
			Status: status, Trigger: trigger,
			StartedAt: startedAt, FinishedAt: finishedAt,
			DurationMs: durationMs, RowsWritten: rowsWritten,
			Error: errText, LogsS3Path: logsS3Path,
			CreatedAt: createdAt,
		}))
	}
	if result == nil {
		result = []domain.Run{}
	}
	return result, rows.Err()
}

// CountRuns returns the total count of runs matching the filter (ignoring Limit/Offset).
func (s *RunStore) CountRuns(ctx context.Context, filter api.RunFilter) (int, error) {
	where, args, _ := runWhereClause(filter)
	query := `SELECT COUNT(*) FROM runs r JOIN pipelines p ON r.pipeline_id = p.id` + where

	var count int
	err := s.pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count runs: %w", err)
	}
	return count, nil
}

func (s *RunStore) GetRun(ctx context.Context, runID string) (*domain.Run, error) {
	id, err := uuid.Parse(runID)
	if err != nil {
		return nil, nil
	}

	row, err := s.q.GetRun(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get run: %w", err)
	}

	run := runRowToDomain(gen.Run{
		ID:          row.ID,
		PipelineID:  row.PipelineID,
		Status:      row.Status,
		Trigger:     row.Trigger,
		StartedAt:   row.StartedAt,
		FinishedAt:  row.FinishedAt,
		DurationMs:  row.DurationMs,
		RowsWritten: row.RowsWritten,
		Error:       row.Error,
		LogsS3Path:  row.LogsS3Path,
		CreatedAt:   row.CreatedAt,
	})
	return &run, nil
}

func (s *RunStore) CreateRun(ctx context.Context, run *domain.Run) error {
	row, err := s.q.CreateRun(ctx, gen.CreateRunParams{
		PipelineID: run.PipelineID,
		Status:     string(run.Status),
		Trigger:    run.Trigger,
	})
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	run.ID = row.ID
	run.CreatedAt = row.CreatedAt
	return nil
}

func (s *RunStore) UpdateRunStatus(ctx context.Context, runID string, status domain.RunStatus, errMsg *string, durationMs *int64, rowsWritten *int64) error {
	id, err := uuid.Parse(runID)
	if err != nil {
		return fmt.Errorf("invalid run id: %w", err)
	}

	params := gen.UpdateRunStatusParams{
		ID:     id,
		Status: string(status),
		Error:  textPtrToNullable(errMsg),
	}
	if durationMs != nil {
		params.DurationMs = pgtype.Int4{Int32: clampInt64ToInt32(*durationMs), Valid: true}
	}
	if rowsWritten != nil {
		params.RowsWritten = pgtype.Int8{Int64: *rowsWritten, Valid: true}
	}
	if err := s.q.UpdateRunStatus(ctx, params); err != nil {
		return err
	}

	// Publish run_completed event for terminal statuses so downstream
	// consumers (trigger evaluator, future SSE push, etc.) can react instantly.
	if s.EventBus != nil && isTerminalStatus(status) {
		// Best-effort: event publishing failure should not fail the status update.
		run, lookupErr := s.q.GetRun(ctx, id)
		if lookupErr == nil {
			_ = s.EventBus.Publish(ctx, ChannelRunCompleted, RunCompletedPayload{
				RunID:      runID,
				PipelineID: run.PipelineID.String(),
				Status:     string(status),
			})
		}
	}

	return nil
}

// isTerminalStatus returns true if the run status is a final state.
func isTerminalStatus(s domain.RunStatus) bool {
	return s == domain.RunStatusSuccess || s == domain.RunStatusFailed || s == domain.RunStatusCancelled
}

// GetRunLogs returns persisted logs from the JSONB column, or empty if not yet saved.
func (s *RunStore) GetRunLogs(ctx context.Context, runID string) ([]api.LogEntry, error) {
	id, err := uuid.Parse(runID)
	if err != nil {
		return []api.LogEntry{}, nil
	}

	data, err := s.q.GetRunLogsByID(ctx, id)
	if err != nil || data == nil {
		return []api.LogEntry{}, nil
	}

	var logs []api.LogEntry
	if err := json.Unmarshal(data, &logs); err != nil {
		return []api.LogEntry{}, nil
	}
	return logs, nil
}

// SaveRunLogs persists pipeline logs as JSONB on the run record.
func (s *RunStore) SaveRunLogs(ctx context.Context, runID string, logs []api.LogEntry) error {
	id, err := uuid.Parse(runID)
	if err != nil {
		return fmt.Errorf("invalid run id: %w", err)
	}

	data, err := json.Marshal(logs)
	if err != nil {
		return fmt.Errorf("marshal logs: %w", err)
	}

	return s.q.SaveRunLogs(ctx, gen.SaveRunLogsParams{
		ID:   id,
		Logs: data,
	})
}

func runRowToDomain(r gen.Run) domain.Run {
	run := domain.Run{
		ID:         r.ID,
		PipelineID: r.PipelineID,
		Status:     domain.RunStatus(r.Status),
		Trigger:    r.Trigger,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		CreatedAt:  r.CreatedAt,
	}
	if r.DurationMs.Valid {
		v := int(r.DurationMs.Int32)
		run.DurationMs = &v
	}
	if r.RowsWritten.Valid {
		v := r.RowsWritten.Int64
		run.RowsWritten = &v
	}
	if r.Error.Valid {
		run.Error = &r.Error.String
	}
	if r.LogsS3Path.Valid {
		run.LogsS3Path = &r.LogsS3Path.String
	}
	return run
}

// DeleteRunsBeyondLimit deletes the oldest runs for a pipeline, keeping the most recent keepCount.
// Returns the number of runs deleted.
func (s *RunStore) DeleteRunsBeyondLimit(ctx context.Context, pipelineID uuid.UUID, keepCount int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM runs WHERE id IN (
			SELECT id FROM runs WHERE pipeline_id = $1
			ORDER BY created_at DESC
			OFFSET $2
		)`, pipelineID, keepCount)
	if err != nil {
		return 0, fmt.Errorf("delete runs beyond limit: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// DeleteRunsOlderThan deletes runs (in terminal states) older than the given time.
// Returns the number of runs deleted.
func (s *RunStore) DeleteRunsOlderThan(ctx context.Context, olderThan time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM runs WHERE created_at < $1 AND status IN ('success', 'failed', 'cancelled')`,
		olderThan)
	if err != nil {
		return 0, fmt.Errorf("delete old runs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// LatestRunPerPipeline returns the most recent run for each of the given pipeline IDs
// in a single query using DISTINCT ON, avoiding N+1 queries for lineage.
func (s *RunStore) LatestRunPerPipeline(ctx context.Context, pipelineIDs []uuid.UUID) (map[uuid.UUID]*domain.Run, error) {
	if len(pipelineIDs) == 0 {
		return map[uuid.UUID]*domain.Run{}, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (r.pipeline_id)
		        r.id, r.pipeline_id, r.status, r.trigger, r.started_at, r.finished_at,
		        r.duration_ms, r.rows_written, r.error, r.logs_s3_path, r.created_at
		 FROM runs r
		 WHERE r.pipeline_id = ANY($1)
		 ORDER BY r.pipeline_id, r.created_at DESC`,
		pipelineIDs)
	if err != nil {
		return nil, fmt.Errorf("latest run per pipeline: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]*domain.Run, len(pipelineIDs))
	for rows.Next() {
		var (
			id, pipelineID        uuid.UUID
			status, trigger       string
			startedAt, finishedAt *time.Time
			durationMs            pgtype.Int4
			rowsWritten           pgtype.Int8
			errText               pgtype.Text
			logsS3Path            pgtype.Text
			createdAt             time.Time
		)
		if err := rows.Scan(&id, &pipelineID, &status, &trigger,
			&startedAt, &finishedAt, &durationMs, &rowsWritten,
			&errText, &logsS3Path, &createdAt); err != nil {
			return nil, fmt.Errorf("scan latest run: %w", err)
		}
		run := runRowToDomain(gen.Run{
			ID: id, PipelineID: pipelineID,
			Status: status, Trigger: trigger,
			StartedAt: startedAt, FinishedAt: finishedAt,
			DurationMs: durationMs, RowsWritten: rowsWritten,
			Error: errText, LogsS3Path: logsS3Path,
			CreatedAt: createdAt,
		})
		result[pipelineID] = &run
	}
	return result, rows.Err()
}

// ListStuckRuns returns runs in pending or running state created before the given cutoff.
func (s *RunStore) ListStuckRuns(ctx context.Context, olderThan time.Time) ([]domain.Run, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, pipeline_id, status, trigger, started_at, finished_at,
		        duration_ms, rows_written, error, logs_s3_path, created_at
		 FROM runs
		 WHERE status IN ('pending', 'running') AND created_at < $1`,
		olderThan)
	if err != nil {
		return nil, fmt.Errorf("list stuck runs: %w", err)
	}
	defer rows.Close()

	var result []domain.Run
	for rows.Next() {
		var (
			id, pipelineID          uuid.UUID
			status, trigger         string
			startedAt, finishedAt   *time.Time
			durationMs              pgtype.Int4
			rowsWritten             pgtype.Int8
			errText                 pgtype.Text
			logsS3Path              pgtype.Text
			createdAt               time.Time
		)
		if err := rows.Scan(&id, &pipelineID, &status, &trigger,
			&startedAt, &finishedAt, &durationMs, &rowsWritten,
			&errText, &logsS3Path, &createdAt); err != nil {
			return nil, fmt.Errorf("scan stuck run: %w", err)
		}
		run := domain.Run{
			ID: id, PipelineID: pipelineID,
			Status: domain.RunStatus(status), Trigger: trigger,
			StartedAt: startedAt, FinishedAt: finishedAt, CreatedAt: createdAt,
		}
		if durationMs.Valid {
			v := int(durationMs.Int32)
			run.DurationMs = &v
		}
		if rowsWritten.Valid {
			v := rowsWritten.Int64
			run.RowsWritten = &v
		}
		if errText.Valid {
			run.Error = &errText.String
		}
		if logsS3Path.Valid {
			run.LogsS3Path = &logsS3Path.String
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

// clampInt64ToInt32 safely narrows an int64 to int32 by clamping to the int32
// range. The DB column duration_ms is INT4 (max ~2.1B ms ≈ 24.8 days), which
// is sufficient for any realistic pipeline run.
func clampInt64ToInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}
