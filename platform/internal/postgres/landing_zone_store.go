package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// LandingZoneStore implements api.LandingZoneStore backed by Postgres.
type LandingZoneStore struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

// NewLandingZoneStore creates a LandingZoneStore backed by the given pool.
func NewLandingZoneStore(pool *pgxpool.Pool) *LandingZoneStore {
	return &LandingZoneStore{pool: pool, q: gen.New(pool)}
}

func (s *LandingZoneStore) ListZones(ctx context.Context, filter api.LandingZoneFilter) ([]api.LandingZoneListItem, error) {
	rows, err := s.q.ListLandingZones(ctx, textOrNull(filter.Namespace))
	if err != nil {
		return nil, fmt.Errorf("list landing zones: %w", err)
	}

	result := make([]api.LandingZoneListItem, len(rows))
	for i, r := range rows {
		result[i] = api.LandingZoneListItem{
			LandingZone: domain.LandingZone{
				ID:             r.ID,
				Namespace:      r.Namespace,
				Name:           r.Name,
				Description:    r.Description,
				Owner:          nullableTextToPtr(r.Owner),
				ExpectedSchema: r.ExpectedSchema,
				CreatedAt:      r.CreatedAt,
				UpdatedAt:      r.UpdatedAt,
			},
			FileCount:  int(r.FileCount),
			TotalBytes: r.TotalBytes,
		}
	}
	return result, nil
}

func (s *LandingZoneStore) GetZone(ctx context.Context, namespace, name string) (*api.LandingZoneDetail, error) {
	row, err := s.q.GetLandingZone(ctx, gen.GetLandingZoneParams{
		Namespace: namespace,
		Name:      name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get landing zone: %w", err)
	}

	return &api.LandingZoneDetail{
		LandingZone: domain.LandingZone{
			ID:             row.ID,
			Namespace:      row.Namespace,
			Name:           row.Name,
			Description:    row.Description,
			Owner:          nullableTextToPtr(row.Owner),
			ExpectedSchema: row.ExpectedSchema,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		},
		FileCount:  int(row.FileCount),
		TotalBytes: row.TotalBytes,
	}, nil
}

func (s *LandingZoneStore) CreateZone(ctx context.Context, z *domain.LandingZone) error {
	row, err := s.q.CreateLandingZone(ctx, gen.CreateLandingZoneParams{
		Namespace:   z.Namespace,
		Name:        z.Name,
		Description: z.Description,
		Owner:       textPtrToNullable(z.Owner),
	})
	if err != nil {
		return fmt.Errorf("landing zone %s/%s already exists", z.Namespace, z.Name)
	}

	z.ID = row.ID
	z.ExpectedSchema = row.ExpectedSchema
	z.CreatedAt = row.CreatedAt
	z.UpdatedAt = row.UpdatedAt
	return nil
}

func (s *LandingZoneStore) DeleteZone(ctx context.Context, namespace, name string) error {
	return s.q.DeleteLandingZone(ctx, gen.DeleteLandingZoneParams{
		Namespace: namespace,
		Name:      name,
	})
}

func (s *LandingZoneStore) UpdateZone(ctx context.Context, namespace, name string, description, owner, expectedSchema *string) (*domain.LandingZone, error) {
	row, err := s.q.UpdateLandingZone(ctx, gen.UpdateLandingZoneParams{
		Namespace:      namespace,
		Name:           name,
		Description:    textPtrToNullable(description),
		Owner:          textPtrToNullable(owner),
		ExpectedSchema: textPtrToNullable(expectedSchema),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update landing zone: %w", err)
	}

	return &domain.LandingZone{
		ID:             row.ID,
		Namespace:      row.Namespace,
		Name:           row.Name,
		Description:    row.Description,
		Owner:          nullableTextToPtr(row.Owner),
		ExpectedSchema: row.ExpectedSchema,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

func (s *LandingZoneStore) ListFiles(ctx context.Context, zoneID uuid.UUID) ([]domain.LandingFile, error) {
	rows, err := s.q.ListLandingFiles(ctx, zoneID)
	if err != nil {
		return nil, fmt.Errorf("list landing files: %w", err)
	}

	result := make([]domain.LandingFile, len(rows))
	for i, r := range rows {
		result[i] = domain.LandingFile{
			ID:          r.ID,
			ZoneID:      r.ZoneID,
			Filename:    r.Filename,
			S3Path:      r.S3Path,
			SizeBytes:   r.SizeBytes,
			ContentType: r.ContentType,
			UploadedBy:  nullableTextToPtr(r.UploadedBy),
			UploadedAt:  r.UploadedAt,
		}
	}
	return result, nil
}

func (s *LandingZoneStore) CreateFile(ctx context.Context, f *domain.LandingFile) error {
	row, err := s.q.CreateLandingFile(ctx, gen.CreateLandingFileParams{
		ZoneID:      f.ZoneID,
		Filename:    f.Filename,
		S3Path:      f.S3Path,
		SizeBytes:   f.SizeBytes,
		ContentType: f.ContentType,
		UploadedBy:  textPtrToNullable(f.UploadedBy),
	})
	if err != nil {
		return fmt.Errorf("create landing file: %w", err)
	}

	f.ID = row.ID
	f.UploadedAt = row.UploadedAt
	return nil
}

func (s *LandingZoneStore) GetFile(ctx context.Context, fileID uuid.UUID) (*domain.LandingFile, error) {
	row, err := s.q.GetLandingFile(ctx, fileID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get landing file: %w", err)
	}

	return &domain.LandingFile{
		ID:          row.ID,
		ZoneID:      row.ZoneID,
		Filename:    row.Filename,
		S3Path:      row.S3Path,
		SizeBytes:   row.SizeBytes,
		ContentType: row.ContentType,
		UploadedBy:  nullableTextToPtr(row.UploadedBy),
		UploadedAt:  row.UploadedAt,
	}, nil
}

func (s *LandingZoneStore) DeleteFile(ctx context.Context, fileID uuid.UUID) error {
	return s.q.DeleteLandingFile(ctx, fileID)
}

func (s *LandingZoneStore) GetZoneByID(ctx context.Context, zoneID uuid.UUID) (*domain.LandingZone, error) {
	row, err := s.q.GetLandingZoneByID(ctx, zoneID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get landing zone by id: %w", err)
	}

	return &domain.LandingZone{
		ID:             row.ID,
		Namespace:      row.Namespace,
		Name:           row.Name,
		Description:    row.Description,
		Owner:          nullableTextToPtr(row.Owner),
		ExpectedSchema: row.ExpectedSchema,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

// UpdateZoneLifecycle updates the lifecycle settings for a landing zone.
func (s *LandingZoneStore) UpdateZoneLifecycle(ctx context.Context, zoneID uuid.UUID, processedMaxAgeDays *int, autoPurge *bool) error {
	query := `UPDATE landing_zones SET updated_at = NOW()`
	args := []interface{}{}
	argN := 1

	if processedMaxAgeDays != nil {
		query += fmt.Sprintf(", processed_max_age_days = $%d", argN)
		args = append(args, *processedMaxAgeDays)
		argN++
	}
	if autoPurge != nil {
		query += fmt.Sprintf(", auto_purge = $%d", argN)
		args = append(args, *autoPurge)
		argN++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argN)
	args = append(args, zoneID)

	_, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update zone lifecycle: %w", err)
	}
	return nil
}

// ListZonesWithAutoPurge returns all landing zones with auto_purge enabled.
func (s *LandingZoneStore) ListZonesWithAutoPurge(ctx context.Context) ([]domain.LandingZone, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, namespace, name, description, owner, expected_schema,
		        processed_max_age_days, auto_purge, created_at, updated_at
		 FROM landing_zones WHERE auto_purge = true`)
	if err != nil {
		return nil, fmt.Errorf("list zones with auto purge: %w", err)
	}
	defer rows.Close()

	var result []domain.LandingZone
	for rows.Next() {
		var z domain.LandingZone
		var owner, desc, schema *string
		var maxAge *int
		if err := rows.Scan(&z.ID, &z.Namespace, &z.Name, &desc, &owner, &schema,
			&maxAge, &z.AutoPurge, &z.CreatedAt, &z.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan zone: %w", err)
		}
		if desc != nil {
			z.Description = *desc
		}
		z.Owner = owner
		if schema != nil {
			z.ExpectedSchema = *schema
		}
		z.ProcessedMaxAgeDays = maxAge
		result = append(result, z)
	}
	return result, rows.Err()
}
