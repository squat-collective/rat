package storage

import (
	"context"
	"fmt"
)

// HealthChecker implements api.HealthChecker for S3/MinIO.
// It checks whether the configured bucket exists and is reachable.
type HealthChecker struct {
	store *S3Store
}

// NewHealthChecker creates an S3 health checker for the given store.
func NewHealthChecker(store *S3Store) *HealthChecker {
	return &HealthChecker{store: store}
}

// HealthCheck verifies S3 connectivity by checking if the bucket exists.
func (h *HealthChecker) HealthCheck(ctx context.Context) error {
	exists, err := h.store.client.BucketExists(ctx, h.store.bucket)
	if err != nil {
		return fmt.Errorf("s3 bucket check: %w", err)
	}
	if !exists {
		return fmt.Errorf("s3 bucket %q does not exist", h.store.bucket)
	}
	return nil
}
