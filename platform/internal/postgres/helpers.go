package postgres

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rat-data/rat/platform/internal/domain"
)

// textOrNull converts a Go string to pgtype.Text.
// Empty string → NULL (invalid), non-empty → valid text.
func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// textPtrToNullable converts a *string to pgtype.Text.
// nil → NULL, non-nil → valid text.
func textPtrToNullable(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// boolPtrToNullable converts a *bool to pgtype.Bool.
func boolPtrToNullable(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

// nullableTextToString converts pgtype.Text to a Go string.
func nullableTextToString(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

// nullableTextToPtr converts pgtype.Text to *string.
func nullableTextToPtr(t pgtype.Text) *string {
	if t.Valid {
		return &t.String
	}
	return nil
}

// pipelineRowToDomain maps a full pipeline row (including versioning columns) to domain.Pipeline.
func pipelineRowToDomain(
	id uuid.UUID, namespace, layer, name, typ, s3Path string,
	description, owner pgtype.Text,
	publishedAt *time.Time, publishedVersions []byte, draftDirty bool,
	maxVersions int,
	createdAt, updatedAt time.Time,
) domain.Pipeline {
	p := domain.Pipeline{
		ID:          id,
		Namespace:   namespace,
		Layer:       domain.Layer(layer),
		Name:        name,
		Type:        typ,
		S3Path:      s3Path,
		Description: nullableTextToString(description),
		Owner:       nullableTextToPtr(owner),
		PublishedAt: publishedAt,
		DraftDirty:  draftDirty,
		MaxVersions: maxVersions,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	if len(publishedVersions) > 0 {
		var pv map[string]string
		if err := json.Unmarshal(publishedVersions, &pv); err == nil {
			p.PublishedVersions = pv
		}
	}

	return p
}
