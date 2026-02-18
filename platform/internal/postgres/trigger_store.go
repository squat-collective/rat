package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// TriggerStore implements api.PipelineTriggerStore backed by Postgres.
type TriggerStore struct {
	q *gen.Queries
}

// NewTriggerStore creates a TriggerStore backed by the given pool.
func NewTriggerStore(pool *pgxpool.Pool) *TriggerStore {
	return &TriggerStore{q: gen.New(pool)}
}

func (s *TriggerStore) ListTriggers(ctx context.Context, pipelineID uuid.UUID) ([]domain.PipelineTrigger, error) {
	rows, err := s.q.ListPipelineTriggers(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}

	result := make([]domain.PipelineTrigger, len(rows))
	for i, r := range rows {
		result[i] = triggerRowToDomain(r)
	}
	return result, nil
}

func (s *TriggerStore) GetTrigger(ctx context.Context, triggerID string) (*domain.PipelineTrigger, error) {
	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return nil, nil
	}

	row, err := s.q.GetPipelineTrigger(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get trigger: %w", err)
	}

	trigger := triggerRowToDomain(row)
	return &trigger, nil
}

func (s *TriggerStore) CreateTrigger(ctx context.Context, trigger *domain.PipelineTrigger) error {
	row, err := s.q.CreatePipelineTrigger(ctx, gen.CreatePipelineTriggerParams{
		PipelineID:      trigger.PipelineID,
		Type:            string(trigger.Type),
		Config:          trigger.Config,
		Enabled:         trigger.Enabled,
		CooldownSeconds: int32(trigger.CooldownSeconds),
	})
	if err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}

	trigger.ID = row.ID
	trigger.CreatedAt = row.CreatedAt
	trigger.UpdatedAt = row.UpdatedAt
	return nil
}

func (s *TriggerStore) UpdateTrigger(ctx context.Context, triggerID string, update api.UpdateTriggerRequest) (*domain.PipelineTrigger, error) {
	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return nil, nil
	}

	params := gen.UpdatePipelineTriggerParams{
		ID:      uid,
		Enabled: boolPtrToNullable(update.Enabled),
	}

	if update.Config != nil {
		params.Config = *update.Config
	}

	if update.CooldownSeconds != nil {
		params.CooldownSeconds = pgtype.Int4{Int32: int32(*update.CooldownSeconds), Valid: true}
	}

	row, err := s.q.UpdatePipelineTrigger(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update trigger: %w", err)
	}

	trigger := triggerRowToDomain(row)
	return &trigger, nil
}

func (s *TriggerStore) DeleteTrigger(ctx context.Context, triggerID string) error {
	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return fmt.Errorf("invalid trigger id: %w", err)
	}
	return s.q.DeletePipelineTrigger(ctx, uid)
}

func (s *TriggerStore) FindTriggersByLandingZone(ctx context.Context, namespace, zoneName string) ([]domain.PipelineTrigger, error) {
	configFilter, _ := json.Marshal(map[string]string{
		"namespace": namespace,
		"zone_name": zoneName,
	})

	rows, err := s.q.FindTriggersByLandingZone(ctx, configFilter)
	if err != nil {
		return nil, fmt.Errorf("find triggers by landing zone: %w", err)
	}

	result := make([]domain.PipelineTrigger, len(rows))
	for i, r := range rows {
		result[i] = triggerRowToDomain(r)
	}
	return result, nil
}

func (s *TriggerStore) FindTriggersByType(ctx context.Context, triggerType string) ([]domain.PipelineTrigger, error) {
	rows, err := s.q.FindTriggersByType(ctx, triggerType)
	if err != nil {
		return nil, fmt.Errorf("find triggers by type: %w", err)
	}
	result := make([]domain.PipelineTrigger, len(rows))
	for i, r := range rows {
		result[i] = triggerRowToDomain(r)
	}
	return result, nil
}

// FindTriggerByWebhookToken looks up a webhook trigger by the SHA-256 hash of
// its token (hex-encoded). The caller must hash the incoming plaintext token
// via api.HashWebhookToken before calling this method.
func (s *TriggerStore) FindTriggerByWebhookToken(ctx context.Context, token string) (*domain.PipelineTrigger, error) {
	row, err := s.q.FindTriggerByWebhookToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find trigger by webhook token: %w", err)
	}
	trigger := triggerRowToDomain(row)
	return &trigger, nil
}

func (s *TriggerStore) FindTriggersByPipelineSuccess(ctx context.Context, namespace, layer, pipeline string) ([]domain.PipelineTrigger, error) {
	rows, err := s.q.FindTriggersByPipelineSuccess(ctx, gen.FindTriggersByPipelineSuccessParams{
		Namespace: namespace,
		Layer:     layer,
		Pipeline:  pipeline,
	})
	if err != nil {
		return nil, fmt.Errorf("find triggers by pipeline success: %w", err)
	}
	result := make([]domain.PipelineTrigger, len(rows))
	for i, r := range rows {
		result[i] = triggerRowToDomain(r)
	}
	return result, nil
}

func (s *TriggerStore) FindTriggersByFilePattern(ctx context.Context, namespace, zoneName string) ([]domain.PipelineTrigger, error) {
	rows, err := s.q.FindTriggersByFilePattern(ctx, gen.FindTriggersByFilePatternParams{
		Namespace: namespace,
		ZoneName:  zoneName,
	})
	if err != nil {
		return nil, fmt.Errorf("find triggers by file pattern: %w", err)
	}
	result := make([]domain.PipelineTrigger, len(rows))
	for i, r := range rows {
		result[i] = triggerRowToDomain(r)
	}
	return result, nil
}

func (s *TriggerStore) UpdateTriggerFired(ctx context.Context, triggerID string, runID uuid.UUID) error {
	uid, err := uuid.Parse(triggerID)
	if err != nil {
		return fmt.Errorf("invalid trigger id: %w", err)
	}
	return s.q.UpdateTriggerFired(ctx, gen.UpdateTriggerFiredParams{
		ID:        uid,
		LastRunID: pgtype.UUID{Bytes: runID, Valid: true},
	})
}

func triggerRowToDomain(r gen.PipelineTrigger) domain.PipelineTrigger {
	trigger := domain.PipelineTrigger{
		ID:              r.ID,
		PipelineID:      r.PipelineID,
		Type:            domain.TriggerType(r.Type),
		Config:          r.Config,
		Enabled:         r.Enabled,
		CooldownSeconds: int(r.CooldownSeconds),
		LastTriggeredAt: r.LastTriggeredAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
	if r.LastRunID.Valid {
		id := uuid.UUID(r.LastRunID.Bytes)
		trigger.LastRunID = &id
	}
	return trigger
}
