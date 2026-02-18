package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// ScheduleStore implements api.ScheduleStore backed by Postgres.
type ScheduleStore struct {
	q *gen.Queries
}

// NewScheduleStore creates a ScheduleStore backed by the given pool.
func NewScheduleStore(pool *pgxpool.Pool) *ScheduleStore {
	return &ScheduleStore{q: gen.New(pool)}
}

func (s *ScheduleStore) ListSchedules(ctx context.Context) ([]domain.Schedule, error) {
	rows, err := s.q.ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}

	result := make([]domain.Schedule, len(rows))
	for i, r := range rows {
		result[i] = scheduleRowToDomain(r)
	}
	return result, nil
}

func (s *ScheduleStore) GetSchedule(ctx context.Context, id string) (*domain.Schedule, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil
	}

	row, err := s.q.GetSchedule(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get schedule: %w", err)
	}

	sched := scheduleRowToDomain(row)
	return &sched, nil
}

func (s *ScheduleStore) CreateSchedule(ctx context.Context, schedule *domain.Schedule) error {
	row, err := s.q.CreateSchedule(ctx, gen.CreateScheduleParams{
		PipelineID: schedule.PipelineID,
		CronExpr:   schedule.CronExpr,
		Enabled:    schedule.Enabled,
	})
	if err != nil {
		return fmt.Errorf("create schedule: %w", err)
	}

	schedule.ID = row.ID
	schedule.CreatedAt = row.CreatedAt
	schedule.UpdatedAt = row.UpdatedAt
	return nil
}

func (s *ScheduleStore) UpdateSchedule(ctx context.Context, id string, update api.UpdateScheduleRequest) (*domain.Schedule, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil
	}

	row, err := s.q.UpdateSchedule(ctx, gen.UpdateScheduleParams{
		ID:       uid,
		CronExpr: textPtrToNullable(update.Cron),
		Enabled:  boolPtrToNullable(update.Enabled),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update schedule: %w", err)
	}

	sched := scheduleRowToDomain(row)
	return &sched, nil
}

func (s *ScheduleStore) UpdateScheduleRun(ctx context.Context, id string, lastRunID string, lastRunAt time.Time, nextRunAt time.Time) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid schedule id: %w", err)
	}

	params := gen.UpdateScheduleRunParams{
		ID:        uid,
		LastRunAt: &lastRunAt,
		NextRunAt: &nextRunAt,
	}

	// lastRunID may be empty when setting initial next_run_at
	if lastRunID != "" {
		runUID, err := uuid.Parse(lastRunID)
		if err != nil {
			return fmt.Errorf("invalid run id: %w", err)
		}
		params.LastRunID = pgtype.UUID{Bytes: runUID, Valid: true}
	}

	return s.q.UpdateScheduleRun(ctx, params)
}

func (s *ScheduleStore) DeleteSchedule(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid schedule id: %w", err)
	}
	return s.q.DeleteSchedule(ctx, uid)
}

func scheduleRowToDomain(r gen.Schedule) domain.Schedule {
	sched := domain.Schedule{
		ID:         r.ID,
		PipelineID: r.PipelineID,
		CronExpr:   r.CronExpr,
		Enabled:    r.Enabled,
		LastRunAt:  r.LastRunAt,
		NextRunAt:  r.NextRunAt,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
	if r.LastRunID.Valid {
		id := uuid.UUID(r.LastRunID.Bytes)
		sched.LastRunID = &id
	}
	return sched
}
