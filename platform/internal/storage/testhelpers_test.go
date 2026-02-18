package storage_test

import (
	"context"
	"os"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rat-data/rat/platform/internal/storage"
)

const testBucket = "rat-test"

// testS3Store returns an S3Store connected to a test MinIO instance.
// It skips the test if S3_ENDPOINT is not set (so `make test-go` stays fast).
// It cleans the bucket before returning.
func testS3Store(t *testing.T) *storage.S3Store {
	t.Helper()

	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_ENDPOINT not set, skipping integration test")
	}

	accessKey := os.Getenv("S3_ACCESS_KEY")
	if accessKey == "" {
		t.Skip("S3_ACCESS_KEY not set, skipping integration test")
	}
	secretKey := os.Getenv("S3_SECRET_KEY")
	if secretKey == "" {
		t.Skip("S3_SECRET_KEY not set, skipping integration test")
	}

	ctx := context.Background()

	store, err := storage.NewS3Store(ctx, endpoint, accessKey, secretKey, testBucket, false)
	if err != nil {
		t.Fatalf("create s3 store: %v", err)
	}

	cleanBucket(t, endpoint, accessKey, secretKey)
	return store
}

// testS3StoreFromConfig returns an S3Store with custom config overrides.
// Connection fields (endpoint, keys, bucket) are filled from env vars;
// the provided cfg is used only for timeout overrides.
func testS3StoreFromConfig(t *testing.T, cfg storage.S3Config) *storage.S3Store {
	t.Helper()

	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_ENDPOINT not set, skipping integration test")
	}
	accessKey := os.Getenv("S3_ACCESS_KEY")
	if accessKey == "" {
		t.Skip("S3_ACCESS_KEY not set, skipping integration test")
	}
	secretKey := os.Getenv("S3_SECRET_KEY")
	if secretKey == "" {
		t.Skip("S3_SECRET_KEY not set, skipping integration test")
	}

	cfg.Endpoint = endpoint
	cfg.AccessKey = accessKey
	cfg.SecretKey = secretKey
	cfg.Bucket = testBucket
	cfg.UseSSL = false

	ctx := context.Background()
	store, err := storage.NewS3StoreFromConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create s3 store from config: %v", err)
	}

	cleanBucket(t, endpoint, accessKey, secretKey)
	return store
}

// cleanBucket removes all objects from the test bucket.
func cleanBucket(t *testing.T, endpoint, accessKey, secretKey string) {
	t.Helper()

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("create minio client for cleanup: %v", err)
	}

	ctx := context.Background()
	objects := client.ListObjects(ctx, testBucket, minio.ListObjectsOptions{Recursive: true})
	for obj := range objects {
		if obj.Err != nil {
			t.Fatalf("list objects for cleanup: %v", obj.Err)
		}
		if err := client.RemoveObject(ctx, testBucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatalf("remove object %s: %v", obj.Key, err)
		}
	}
}
