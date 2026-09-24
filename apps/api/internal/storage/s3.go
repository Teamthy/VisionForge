package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/visionforge/visionforge/packages/config"
)

// S3Storage implements ObjectStorage for S3-compatible systems (AWS S3, R2, MinIO).
type S3Storage struct {
	client *minio.Client
	bucket string
	secure bool
}

// NewS3 constructs an S3/MinIO-backed ObjectStorage and ensures the bucket exists.
func NewS3(ctx context.Context, cfg config.StorageConfig) (*S3Storage, error) {
	endpoint, secure, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}
	s := &S3Storage{client: client, bucket: cfg.Bucket, secure: secure}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket exists check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("make bucket: %w", err)
		}
	}
	return s, nil
}

func parseEndpoint(endpoint string) (host string, secure bool, err error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, err
	}
	host = u.Host
	secure = u.Scheme == "https"
	return
}

func (s *S3Storage) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	if size < 0 {
		size = -1
	}
	opts := minio.PutObjectOptions{ContentType: contentType}
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, opts)
	return err
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, map[string]string, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, err
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, nil, mapMinioErr(err)
	}
	return obj, map[string]string{"Content-Type": stat.ContentType, "Size": fmt.Sprintf("%d", stat.Size)}, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	// Not-found is not an error for our contract.
	return err
}

func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Storage) GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *S3Storage) GetPresignedPUT(ctx context.Context, key, contentType string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedPutObject(ctx, s.bucket, key, expiry)
	if err != nil {
		return "", err
	}
	// MinIO's presigned PUT doesn't encode Content-Type in URL; clients must send Content-Type header.
	return u.String(), nil
}

func (s *S3Storage) Health(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucket)
	return err
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var merr minio.ErrorResponse
	if errors.As(err, &merr) {
		return merr.Code == "NoSuchKey" || merr.StatusCode == 404
	}
	// minio-go v7 returns ErrorResponse for most errors; also check via string message.
	errResp := minio.ToErrorResponse(err)
	if errResp.StatusCode == 404 || errResp.Code == "NoSuchKey" {
		return true
	}
	return false
}

func mapMinioErr(err error) error {
	if isNotFound(err) {
		return fmt.Errorf("object not found: %w", err)
	}
	return err
}
