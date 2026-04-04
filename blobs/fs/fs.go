package fsblob

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mastodon-site/activitypub-core/blobs"
)

// FS implements blobs.Store on a local directory.
type FS struct {
	Root string
}

// New creates a filesystem blob store under root (must exist or first Put creates parent dirs).
func New(root string) *FS {
	return &FS{Root: root}
}

var _ blobs.Store = (*FS)(nil)

func (f *FS) safePath(key string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	return filepath.Join(f.Root, clean), nil
}

// Put writes object; creates parent directories.
func (f *FS) Put(ctx context.Context, key string, contentType string, r io.Reader, size int64) error {
	path, err := f.safePath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	w, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		os.Remove(tmp)
		return err
	}
	if err := w.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	select {
	case <-ctx.Done():
		os.Remove(tmp)
		return ctx.Err()
	default:
	}
	return os.Rename(tmp, path)
}

// Get opens a blob for reading.
func (f *FS) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	path, err := f.safePath(key)
	if err != nil {
		return nil, "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	return file, "application/octet-stream", nil
}

// Delete removes a blob.
func (f *FS) Delete(ctx context.Context, key string) error {
	path, err := f.safePath(key)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
