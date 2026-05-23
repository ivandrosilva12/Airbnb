// Package port defines outbound ports (interfaces) used by application
// services but implemented in the infrastructure layer.
package port

import (
	"context"
	"io"
	"time"
)

// Storage abstracts an object store (implemented by MinIO/S3).
type Storage interface {
	// Upload streams an object and returns its public URL.
	Upload(ctx context.Context, objectKey string, r io.Reader, size int64, contentType string) (string, error)
	// PresignedPutURL returns a URL the client can PUT directly to.
	PresignedPutURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error)
	// PublicURL builds the public URL for a stored object key.
	PublicURL(objectKey string) string
}
