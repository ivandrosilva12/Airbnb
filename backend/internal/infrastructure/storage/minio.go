// Package storage provides a MinIO/S3-backed implementation of the storage port.
package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/airhost/backend/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioStorage implements port.Storage on top of a MinIO/S3 bucket.
type MinioStorage struct {
	client     *minio.Client
	bucket     string
	publicHost string
}

// NewMinioStorage connects to MinIO and ensures the target bucket exists.
func NewMinioStorage(ctx context.Context, cfg config.StorageConfig) (*MinioStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
		// Allow public read of objects so generated URLs are directly viewable.
		policy := fmt.Sprintf(`{
			"Version":"2012-10-17",
			"Statement":[{
				"Effect":"Allow",
				"Principal":{"AWS":["*"]},
				"Action":["s3:GetObject"],
				"Resource":["arn:aws:s3:::%s/*"]
			}]
		}`, cfg.Bucket)
		_ = client.SetBucketPolicy(ctx, cfg.Bucket, policy)
	}

	return &MinioStorage{
		client:     client,
		bucket:     cfg.Bucket,
		publicHost: strings.TrimRight(cfg.PublicHost, "/"),
	}, nil
}

// Upload streams an object into the bucket and returns its public URL.
func (s *MinioStorage) Upload(ctx context.Context, objectKey string, r io.Reader, size int64, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, s.bucket, objectKey, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("put object: %w", err)
	}
	return s.PublicURL(objectKey), nil
}

// PresignedPutURL returns a temporary URL the client can PUT directly to.
func (s *MinioStorage) PresignedPutURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedPutObject(ctx, s.bucket, objectKey, expiry)
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return u.String(), nil
}

// PublicURL builds the public URL for an object key.
func (s *MinioStorage) PublicURL(objectKey string) string {
	return fmt.Sprintf("%s/%s/%s", s.publicHost, s.bucket, url.PathEscape(objectKey))
}
