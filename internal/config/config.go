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
	// LocalUsername is the first entry in LocalUsernames (backward compatible default signing user).
	LocalUsername string
	// LocalUsernames lists every actor identity on this instance (AP_LOCAL_USERNAMES comma-separated, else AP_LOCAL_USERNAME).
	LocalUsernames []string

	// FollowAutoAccept enqueues Accept for inbound Follow activities (AP_FOLLOW_AUTO_ACCEPT, default true).
	FollowAutoAccept bool

	// ActorPrivateKeyPath is a PEM file (PKCS#1 or PKCS#8 RSA private key) for the local actor.
	// If set, AP_ACTOR_PUBLIC_KEY_PATH may point to a PKIX public PEM; otherwise the public key is derived.
	ActorPrivateKeyPath string
	ActorPublicKeyPath  string

	// InboxMaxBody is the max bytes read for POST /inbox (HTTP Signature + activity body).
	InboxMaxBody int

	// FetchAllowHTTP allows http:// for outbound federation fetches (AP_FETCH_ALLOW_HTTP).
	// Default false: HTTPS only (recommended on the public internet).
	FetchAllowHTTP bool

	// OutboxPostSecret, if set, enables POST /@{user}/outbox with Authorization: Bearer <secret>.
	OutboxPostSecret string

	// MediaUploadSecret enables POST /media with Authorization: Bearer <secret>.
	MediaUploadSecret   string
	MediaMaxUploadBytes int

	// RequireAuthorizedFetch rejects unsigned GET for federation objects when true (AP_REQUIRE_AUTHORIZED_FETCH).
	RequireAuthorizedFetch bool
	// SignOutboundGET signs outbound federation actor GETs as the instance actor (AP_SIGN_GET).
	SignOutboundGET bool
	// InstanceActorPrivateKeyPath, if set, PEM for instance actor signed fetch; else ActorPrivateKeyPath is used.
	InstanceActorPrivateKeyPath string

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
		HTTPListen:                  getenv("AP_HTTP_LISTEN", ":8080"),
		MetricsListen:               os.Getenv("AP_METRICS_LISTEN"),
		DatabaseURL:                 os.Getenv("AP_DATABASE_URL"),
		RedisURL:                    os.Getenv("AP_REDIS_URL"),
		QueueBackend:                strings.ToLower(getenv("AP_QUEUE_BACKEND", "sql")),
		PublicBaseURL:               strings.TrimRight(os.Getenv("AP_PUBLIC_BASE_URL"), "/"),
		FollowAutoAccept:            true,
		ActorPrivateKeyPath:         os.Getenv("AP_ACTOR_PRIVATE_KEY_PATH"),
		ActorPublicKeyPath:          os.Getenv("AP_ACTOR_PUBLIC_KEY_PATH"),
		InboxMaxBody:                getenvInt("AP_INBOX_MAX_BODY_BYTES", 1<<20),
		OutboxPostSecret:            os.Getenv("AP_OUTBOX_POST_SECRET"),
		MediaUploadSecret:           os.Getenv("AP_MEDIA_UPLOAD_SECRET"),
		MediaMaxUploadBytes:         getenvInt("AP_MEDIA_MAX_UPLOAD_BYTES", 10<<20),
		InstanceActorPrivateKeyPath: os.Getenv("AP_INSTANCE_ACTOR_PRIVATE_KEY_PATH"),
		BlobBackend:                 strings.ToLower(getenv("AP_BLOB_BACKEND", "filesystem")),
		BlobFSRoot:                  getenv("AP_BLOB_FS_ROOT", "./blobdata"),
		BlobS3Bucket:                os.Getenv("AP_BLOB_S3_BUCKET"),
		BlobS3Endpoint:              os.Getenv("AP_BLOB_S3_ENDPOINT"),
		BlobS3Region:                getenv("AP_BLOB_S3_REGION", "us-east-1"),
		WorkerConcurrency:           getenvInt("AP_WORKER_CONCURRENCY", 2),
		WorkerPollInterval:          getenvDuration("AP_WORKER_POLL_INTERVAL", 2*time.Second),
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

	if v := strings.TrimSpace(os.Getenv("AP_LOCAL_USERNAMES")); v != "" {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				c.LocalUsernames = append(c.LocalUsernames, part)
			}
		}
	}
	if len(c.LocalUsernames) == 0 {
		c.LocalUsernames = []string{getenv("AP_LOCAL_USERNAME", "admin")}
	}
	c.LocalUsername = c.LocalUsernames[0]

	if v := strings.ToLower(strings.TrimSpace(os.Getenv("AP_FOLLOW_AUTO_ACCEPT"))); v == "0" || v == "false" || v == "no" {
		c.FollowAutoAccept = false
	}

	if truthyEnv("AP_FETCH_ALLOW_HTTP") {
		c.FetchAllowHTTP = true
	}
	if truthyEnv("AP_REQUIRE_AUTHORIZED_FETCH") {
		c.RequireAuthorizedFetch = true
	}
	if truthyEnv("AP_SIGN_GET") {
		c.SignOutboundGET = true
	}

	return c, nil
}

func truthyEnv(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes"
}

// IsLocalUsername reports whether name is one of this server's actor accounts.
func (c *Config) IsLocalUsername(name string) bool {
	for _, u := range c.LocalUsernames {
		if u == name {
			return true
		}
	}
	// Tests and minimal configs may set only LocalUsername (Load() always fills LocalUsernames).
	if len(c.LocalUsernames) == 0 && c.LocalUsername != "" && c.LocalUsername == name {
		return true
	}
	return false
}

// LocalActorProfileURL returns /@{username} IRI for a local account.
func (c *Config) LocalActorProfileURL(username string) string {
	root := strings.TrimRight(c.PublicBaseURL, "/")
	return root + "/@" + url.PathEscape(username)
}

// LocalActorOutboxURL returns the canonical outbox collection URL for a local user.
func (c *Config) LocalActorOutboxURL(username string) string {
	root := strings.TrimRight(c.PublicBaseURL, "/")
	return root + "/@" + url.PathEscape(username) + "/outbox"
}

// LocalActorFollowersURL returns the followers collection URL for a local user.
func (c *Config) LocalActorFollowersURL(username string) string {
	root := strings.TrimRight(c.PublicBaseURL, "/")
	return root + "/@" + url.PathEscape(username) + "/followers"
}

// LocalActorFollowingURL returns the following collection URL for a local user.
func (c *Config) LocalActorFollowingURL(username string) string {
	root := strings.TrimRight(c.PublicBaseURL, "/")
	return root + "/@" + url.PathEscape(username) + "/following"
}

// InstanceActorIRI returns the canonical instance actor document URL (trailing path normalized).
func (c *Config) InstanceActorIRI() string {
	return strings.TrimRight(c.PublicBaseURL, "/") + "/.well-known/actor"
}

// InstanceActorKeyID returns the HTTP Signatures keyId for the instance actor.
func (c *Config) InstanceActorKeyID() string {
	return c.InstanceActorIRI() + "#main-key"
}

// LocalSharedInboxURL returns the shared inbox IRI for this instance.
func (c *Config) LocalSharedInboxURL() string {
	return strings.TrimRight(c.PublicBaseURL, "/") + "/inbox"
}

// LocalUsernameForActorURL returns the local username if actorURL is one of our profiles.
func (c *Config) LocalUsernameForActorURL(actorURL string) (string, bool) {
	want := strings.TrimRight(strings.TrimSpace(actorURL), "/")
	for _, u := range c.LocalUsernames {
		if strings.TrimRight(c.LocalActorProfileURL(u), "/") == want {
			return u, true
		}
	}
	if len(c.LocalUsernames) == 0 && c.LocalUsername != "" {
		u := c.LocalUsername
		if strings.TrimRight(c.LocalActorProfileURL(u), "/") == want {
			return u, true
		}
	}
	return "", false
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
