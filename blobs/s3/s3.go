package s3blob

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/mastodon-site/activitypub-core/blobs"
)

// S3 implements blobs.Store via AWS SDK v2 (works with MinIO via custom endpoint on client).
type S3 struct {
	Client *s3.Client
	Bucket string
}

var _ blobs.Store = (*S3)(nil)

// Put uploads an object.
func (s *S3) Put(ctx context.Context, key string, contentType string, r io.Reader, size int64) error {
	in := &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
	}
	if size > 0 {
		in.ContentLength = aws.Int64(size)
	}
	_, err := s.Client.PutObject(ctx, in)
	return err
}

// Get downloads an object.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", err
	}
	ct := "application/octet-stream"
	if out.ContentType != nil && *out.ContentType != "" {
		ct = *out.ContentType
	}
	return out.Body, ct, nil
}

// Delete removes an object.
func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
	})
	return err
}

// New returns an S3-backed blob store.
func New(client *s3.Client, bucket string) *S3 {
	return &S3{Client: client, Bucket: bucket}
}
