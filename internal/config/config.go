// Package config loads process configuration from the environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds settings shared by API and worker binaries.
type Config struct {
	HTTPListen    string
	MetricsListen string

	DatabaseURL  string
	RedisURL     string
	QueueBackend string

	PublicBaseURL string
	LocalUsername string

	BlobBackend    string
	BlobFSRoot     string
	BlobS3Bucket   string
	BlobS3Endpoint string
	BlobS3Region   string

	WorkerConcurrency  int
	WorkerPollInterval time.Duration
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	c := &Config{
		HTTPListen:         getenv("AP_HTTP_LISTEN", ":8080"),
		MetricsListen:      os.Getenv("AP_METRICS_LISTEN"),
		DatabaseURL:        os.Getenv("AP_DATABASE_URL"),
		RedisURL:           os.Getenv("AP_REDIS_URL"),
		QueueBackend:       strings.ToLower(getenv("AP_QUEUE_BACKEND", "sql")),
		PublicBaseURL:      strings.TrimRight(os.Getenv("AP_PUBLIC_BASE_URL"), "/"),
		LocalUsername:      getenv("AP_LOCAL_USERNAME", "admin"),
		BlobBackend:        strings.ToLower(getenv("AP_BLOB_BACKEND", "filesystem")),
		BlobFSRoot:         getenv("AP_BLOB_FS_ROOT", "./blobdata"),
		BlobS3Bucket:       os.Getenv("AP_BLOB_S3_BUCKET"),
		BlobS3Endpoint:     os.Getenv("AP_BLOB_S3_ENDPOINT"),
		BlobS3Region:       getenv("AP_BLOB_S3_REGION", "us-east-1"),
		WorkerConcurrency:  getenvInt("AP_WORKER_CONCURRENCY", 2),
		WorkerPollInterval: getenvDuration("AP_WORKER_POLL_INTERVAL", 2*time.Second),
	}
	if c.QueueBackend != "sql" && c.QueueBackend != "redis" {
		return nil, fmt.Errorf("AP_QUEUE_BACKEND must be sql or redis, got %q", c.QueueBackend)
	}
	if c.BlobBackend != "filesystem" && c.BlobBackend != "s3" {
		return nil, fmt.Errorf("AP_BLOB_BACKEND must be filesystem or s3, got %q", c.BlobBackend)
	}
	if c.PublicBaseURL != "" {
		if _, err := url.Parse(c.PublicBaseURL); err != nil {
			return nil, fmt.Errorf("AP_PUBLIC_BASE_URL: %w", err)
		}
	}
	return c, nil
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getenvDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
