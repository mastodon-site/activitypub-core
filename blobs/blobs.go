// Package blobs defines object storage for media and cached remote files.
package blobs

import (
	"context"
	"io"
)

// Store abstracts filesystem or S3-compatible backends.
type Store interface {
	Put(ctx context.Context, key string, contentType string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, string, error)
	Delete(ctx context.Context, key string) error
}
