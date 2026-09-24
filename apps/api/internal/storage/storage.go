// Package storage provides an object-storage abstraction with an S3/MinIO implementation.
package storage

import (
	"context"
	"io"
	"time"
)

// ObjectStorage abstracts over S3-compatible stores (MinIO locally, S3/R2 in production).
type ObjectStorage interface {
	// Put uploads body under key with contentType and optional contentLength (-1 if unknown).
	Put(ctx context.Context, key, contentType string, body io.Reader, contentLength int64) error
	// Get returns a reader for the object. Caller must close.
	Get(ctx context.Context, key string) (io.ReadCloser, map[string]string, error)
	// Delete removes an object (best-effort; no error if not found).
	Delete(ctx context.Context, key string) error
	// Exists checks if an object exists.
	Exists(ctx context.Context, key string) (bool, error)
	// GetPresignedURL returns a presigned GET URL valid for expiry duration.
	GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error)
	// GetPresignedPUT returns a presigned PUT URL for the client to upload directly.
	GetPresignedPUT(ctx context.Context, key, contentType string, expiry time.Duration) (string, error)
	// Health checks connectivity/permission to the bucket.
	Health(ctx context.Context) error
}
