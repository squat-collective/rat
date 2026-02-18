// Package storage implements api.StorageStore backed by S3-compatible object storage.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rat-data/rat/platform/internal/api"
)

// Default timeouts for S3 operations.
const (
	DefaultMetadataTimeout = 10 * time.Second // List, Head, Stat, Delete operations
	DefaultDataTimeout     = 60 * time.Second // Get, Put operations (data transfer)
)

// S3Config holds connection and timeout settings for S3 storage.
type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool

	// MetadataTimeout is the context timeout for metadata operations
	// (list, stat, delete). Defaults to 10s if zero.
	MetadataTimeout time.Duration

	// DataTimeout is the context timeout for data-transfer operations
	// (get, put). Defaults to 60s if zero.
	DataTimeout time.Duration
}

// S3Store implements api.StorageStore using MinIO / S3-compatible storage.
type S3Store struct {
	client          *minio.Client
	bucket          string
	metadataTimeout time.Duration
	dataTimeout     time.Duration
}

// NewS3Store creates an S3Store connected to the given endpoint.
// It auto-creates the bucket if it doesn't exist.
func NewS3Store(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool) (*S3Store, error) {
	return NewS3StoreFromConfig(ctx, S3Config{
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    bucket,
		UseSSL:    useSSL,
	})
}

// NewS3StoreFromConfig creates an S3Store with explicit timeout configuration.
// It configures the underlying HTTP transport with connection and TLS timeouts,
// and applies per-operation context timeouts to all S3 calls.
func NewS3StoreFromConfig(ctx context.Context, cfg S3Config) (*S3Store, error) {
	metadataTimeout := cfg.MetadataTimeout
	if metadataTimeout == 0 {
		metadataTimeout = DefaultMetadataTimeout
	}
	dataTimeout := cfg.DataTimeout
	if dataTimeout == 0 {
		dataTimeout = DefaultDataTimeout
	}

	// Custom transport with explicit dial and TLS timeouts.
	// ResponseHeaderTimeout is set to the metadata timeout — it bounds the
	// time waiting for the server to start replying, not the full download.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: metadataTimeout,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:    cfg.UseSSL,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	s := &S3Store{
		client:          client,
		bucket:          cfg.Bucket,
		metadataTimeout: metadataTimeout,
		dataTimeout:     dataTimeout,
	}

	if err := s.ensureBucket(ctx); err != nil {
		return nil, err
	}

	return s, nil
}

// withMetadataTimeout returns a child context with the metadata operation timeout.
// If the parent already has an earlier deadline, that deadline is preserved.
func (s *S3Store) withMetadataTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.metadataTimeout)
}

// withDataTimeout returns a child context with the data operation timeout.
// If the parent already has an earlier deadline, that deadline is preserved.
func (s *S3Store) withDataTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.dataTimeout)
}

// ensureBucket creates the bucket if it doesn't already exist.
func (s *S3Store) ensureBucket(ctx context.Context) error {
	ctx, cancel := s.withMetadataTimeout(ctx)
	defer cancel()

	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", s.bucket, err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", s.bucket, err)
		}
	}
	return nil
}

// ListFiles returns metadata for all objects matching the given prefix.
// Returns an empty slice (never nil) if no objects match.
func (s *S3Store) ListFiles(ctx context.Context, prefix string) ([]api.FileInfo, error) {
	ctx, cancel := s.withMetadataTimeout(ctx)
	defer cancel()

	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}

	files := make([]api.FileInfo, 0)
	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list objects: %w", obj.Err)
		}
		files = append(files, api.FileInfo{
			Path:     obj.Key,
			Size:     obj.Size,
			Modified: obj.LastModified,
			Type:     detectFileType(obj.Key),
		})
	}

	return files, nil
}

// ReadFile reads a single object's content.
// Returns nil, nil if the object does not exist (not an error).
func (s *S3Store) ReadFile(ctx context.Context, path string) (*api.FileContent, error) {
	ctx, cancel := s.withDataTimeout(ctx)
	defer cancel()

	obj, err := s.client.GetObject(ctx, s.bucket, path, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", path, err)
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil, nil
		}
		return nil, fmt.Errorf("stat object %s: %w", path, err)
	}

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read object %s: %w", path, err)
	}

	return &api.FileContent{
		Path:     path,
		Content:  string(data),
		Size:     info.Size,
		Modified: info.LastModified,
	}, nil
}

// WriteFile creates or overwrites an object with the given content.
// Returns the S3 version ID of the written object (empty if versioning is not enabled).
func (s *S3Store) WriteFile(ctx context.Context, path string, content []byte) (string, error) {
	ctx, cancel := s.withDataTimeout(ctx)
	defer cancel()

	reader := bytes.NewReader(content)
	contentType := detectContentType(path)

	info, err := s.client.PutObject(ctx, s.bucket, path, reader, int64(len(content)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("put object %s: %w", path, err)
	}
	return info.VersionID, nil
}

// ReadFileVersion reads a specific version of a file from S3.
// Returns nil, nil if the version does not exist.
func (s *S3Store) ReadFileVersion(ctx context.Context, path, versionID string) (*api.FileContent, error) {
	ctx, cancel := s.withDataTimeout(ctx)
	defer cancel()

	obj, err := s.client.GetObject(ctx, s.bucket, path, minio.GetObjectOptions{
		VersionID: versionID,
	})
	if err != nil {
		return nil, fmt.Errorf("get object version %s@%s: %w", path, versionID, err)
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.Code == "NoSuchVersion" {
			return nil, nil
		}
		return nil, fmt.Errorf("stat object version %s@%s: %w", path, versionID, err)
	}

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read object version %s@%s: %w", path, versionID, err)
	}

	return &api.FileContent{
		Path:      path,
		Content:   string(data),
		Size:      info.Size,
		Modified:  info.LastModified,
		VersionID: info.VersionID,
	}, nil
}

// StatFile returns metadata about an object without reading its content.
// Returns the current HEAD version ID among other metadata.
func (s *S3Store) StatFile(ctx context.Context, path string) (*api.FileInfo, error) {
	ctx, cancel := s.withMetadataTimeout(ctx)
	defer cancel()

	info, err := s.client.StatObject(ctx, s.bucket, path, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil, nil
		}
		return nil, fmt.Errorf("stat object %s: %w", path, err)
	}
	return &api.FileInfo{
		Path:      info.Key,
		Size:      info.Size,
		Modified:  info.LastModified,
		Type:      detectFileType(info.Key),
		VersionID: info.VersionID,
	}, nil
}

// DeleteFile removes an object. S3 delete is idempotent — deleting a
// non-existent object is not an error. This avoids an unnecessary StatObject
// round-trip before every delete.
func (s *S3Store) DeleteFile(ctx context.Context, path string) error {
	ctx, cancel := s.withMetadataTimeout(ctx)
	defer cancel()

	if err := s.client.RemoveObject(ctx, s.bucket, path, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object %s: %w", path, err)
	}
	return nil
}
