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
	// FetchRelaxLocal uses the same SSRF rules as fetch.TestingPolicy (http + loopback/private
	// targets). Set via AP_FETCH_RELAX_LOCAL or in tests; not for untrusted production instances.
	FetchRelaxLocal bool

	// OutboxPostSecret, if set, enables POST /@{user}/outbox with Authorization: Bearer <secret>.
	OutboxPostSecret string

	// MediaUploadSecret enables POST /media with Authorization: Bearer <secret>.
	MediaUploadSecret   string
	MediaMaxUploadBytes int
	// MediaMaxAttachmentsPerStatus caps media_ids on POST /api/v1/statuses (AP_MEDIA_MAX_ATTACHMENTS_PER_STATUS, default 4).
	MediaMaxAttachmentsPerStatus int
	// MediaAllowedMIMETypes is the upload allowlist for POST /api/v1|v2/media; empty means built-in image defaults.
	MediaAllowedMIMETypes []string
	// MediaAsyncUpload makes POST /api/v2/media return 202 while attachments finish processing in the background (AP_MEDIA_ASYNC_UPLOAD).
	MediaAsyncUpload bool
	// MediaDescriptionLimit is reported in GET /api/v2/instance configuration (AP_MEDIA_DESCRIPTION_LIMIT, default 1500).
	MediaDescriptionLimit int

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

	// InstanceName is a human-readable title for NodeInfo / discovery (AP_INSTANCE_NAME); default: public host.
	InstanceName string
	// InstanceDescription is a short blurb for NodeInfo metadata (AP_INSTANCE_DESCRIPTION).
	InstanceDescription string
	// OpenRegistrations is exposed in NodeInfo when true (AP_OPEN_REGISTRATIONS); not wired to account signup by itself.
	OpenRegistrations bool
	// SoftwareVersion is reported in NodeInfo (AP_SOFTWARE_VERSION).
	SoftwareVersion string

	// CORSAllowOrigins lists permitted browser Origins for cross-origin API access (AP_CORS_ALLOW_ORIGINS,
	// comma-separated, e.g. https://app.example or http://localhost:5173). Use "*" for any origin
	// (no credentials). Empty leaves CORS disabled in apd.
	CORSAllowOrigins []string
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
		MediaMaxAttachmentsPerStatus: getenvInt("AP_MEDIA_MAX_ATTACHMENTS_PER_STATUS", 4),
		MediaDescriptionLimit:        getenvInt("AP_MEDIA_DESCRIPTION_LIMIT", 1500),
		InstanceActorPrivateKeyPath: os.Getenv("AP_INSTANCE_ACTOR_PRIVATE_KEY_PATH"),
		BlobBackend:                 strings.ToLower(getenv("AP_BLOB_BACKEND", "filesystem")),
		BlobFSRoot:                  getenv("AP_BLOB_FS_ROOT", "./blobdata"),
		BlobS3Bucket:                os.Getenv("AP_BLOB_S3_BUCKET"),
		BlobS3Endpoint:              os.Getenv("AP_BLOB_S3_ENDPOINT"),
		BlobS3Region:                getenv("AP_BLOB_S3_REGION", "us-east-1"),
		WorkerConcurrency:           getenvInt("AP_WORKER_CONCURRENCY", 2),
		WorkerPollInterval:          getenvDuration("AP_WORKER_POLL_INTERVAL", 2*time.Second),
		InstanceName:                strings.TrimSpace(os.Getenv("AP_INSTANCE_NAME")),
		InstanceDescription:         strings.TrimSpace(os.Getenv("AP_INSTANCE_DESCRIPTION")),
		SoftwareVersion:             getenv("AP_SOFTWARE_VERSION", "dev"),
	}
	if v := strings.TrimSpace(os.Getenv("AP_CORS_ALLOW_ORIGINS")); v != "" {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				c.CORSAllowOrigins = append(c.CORSAllowOrigins, part)
			}
		}
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
	if truthyEnv("AP_FETCH_RELAX_LOCAL") {
		c.FetchRelaxLocal = true
	}
	if truthyEnv("AP_REQUIRE_AUTHORIZED_FETCH") {
		c.RequireAuthorizedFetch = true
	}
	if truthyEnv("AP_SIGN_GET") {
		c.SignOutboundGET = true
	}
	if truthyEnv("AP_OPEN_REGISTRATIONS") {
		c.OpenRegistrations = true
	}
	if truthyEnv("AP_MEDIA_ASYNC_UPLOAD") {
		c.MediaAsyncUpload = true
	}
	if v := strings.TrimSpace(os.Getenv("AP_MEDIA_ALLOWED_MIME_TYPES")); v != "" {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			if part != "" {
				c.MediaAllowedMIMETypes = append(c.MediaAllowedMIMETypes, part)
			}
		}
	}

	return c, nil
}

// DefaultMediaAllowedMIMETypes is the implicit allowlist when AP_MEDIA_ALLOWED_MIME_TYPES is unset.
func DefaultMediaAllowedMIMETypes() []string {
	return []string{"image/jpeg", "image/png", "image/gif", "image/webp"}
}

// EffectiveMediaAllowedMIMETypes returns the configured allowlist or the default image types.
func (c *Config) EffectiveMediaAllowedMIMETypes() []string {
	if c == nil || len(c.MediaAllowedMIMETypes) == 0 {
		return DefaultMediaAllowedMIMETypes()
	}
	return c.MediaAllowedMIMETypes
}

// EffectiveMediaMaxAttachmentsPerStatus returns the cap for media_ids on new statuses (minimum 1).
func (c *Config) EffectiveMediaMaxAttachmentsPerStatus() int {
	n := 4
	if c != nil && c.MediaMaxAttachmentsPerStatus > 0 {
		n = c.MediaMaxAttachmentsPerStatus
	}
	if n < 1 {
		return 1
	}
	return n
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

// IsLocalActorFollowersOrFollowingCollectionIRI reports whether ref is this instance's followers or
// following collection for a configured local user. These are OrderedCollection documents, not Actor;
// they must not be passed to inbox resolution (no "inbox" property).
func (c *Config) IsLocalActorFollowersOrFollowingCollectionIRI(ref string) bool {
	if c == nil || strings.TrimSpace(c.PublicBaseURL) == "" {
		return false
	}
	wantU, err := url.Parse(strings.TrimSpace(ref))
	if err != nil || wantU.Host == "" {
		return false
	}
	baseU, err := url.Parse(strings.TrimRight(c.PublicBaseURL, "/"))
	if err != nil || baseU.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(wantU.Hostname(), baseU.Hostname()) {
		return false
	}
	wantPath := strings.Trim(wantU.Path, "/")
	usernames := c.LocalUsernames
	if len(usernames) == 0 && strings.TrimSpace(c.LocalUsername) != "" {
		usernames = []string{c.LocalUsername}
	}
	for _, u := range usernames {
		fu, err := url.Parse(c.LocalActorFollowersURL(u))
		if err == nil && strings.Trim(fu.Path, "/") == wantPath {
			return true
		}
		flu, err := url.Parse(c.LocalActorFollowingURL(u))
		if err == nil && strings.Trim(flu.Path, "/") == wantPath {
			return true
		}
	}
	return false
}

// LocalActorInboxURL returns the per-actor inbox IRI (POST target). Shared delivery still uses endpoints.sharedInbox.
func (c *Config) LocalActorInboxURL(username string) string {
	root := strings.TrimRight(c.PublicBaseURL, "/")
	return root + "/@" + url.PathEscape(username) + "/inbox"
}

// IsAddressingThisInstanceInbox reports whether ref is the shared inbox or any local actor's inbox IRI.
func (c *Config) IsAddressingThisInstanceInbox(ref string) bool {
	if c == nil || strings.TrimSpace(c.PublicBaseURL) == "" {
		return false
	}
	r := strings.TrimRight(strings.TrimSpace(ref), "/")
	if r == "" {
		return false
	}
	if r == strings.TrimRight(c.LocalSharedInboxURL(), "/") {
		return true
	}
	for _, u := range c.LocalUsernames {
		if r == strings.TrimRight(c.LocalActorInboxURL(u), "/") {
			return true
		}
	}
	if len(c.LocalUsernames) == 0 && strings.TrimSpace(c.LocalUsername) != "" {
		return r == strings.TrimRight(c.LocalActorInboxURL(c.LocalUsername), "/")
	}
	return false
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

// WebFingerTemplateURL is the RFC 7033 lrdd template (host-meta / WebFinger discovery).
func (c *Config) WebFingerTemplateURL() string {
	return strings.TrimRight(strings.TrimSpace(c.PublicBaseURL), "/") + "/.well-known/webfinger?resource={uri}"
}

// InstanceDisplayName returns AP_INSTANCE_NAME or the public hostname from AP_PUBLIC_BASE_URL.
func (c *Config) InstanceDisplayName() string {
	if strings.TrimSpace(c.InstanceName) != "" {
		return strings.TrimSpace(c.InstanceName)
	}
	u, err := url.Parse(strings.TrimSpace(c.PublicBaseURL))
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "activitypub-core"
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

// LocalUsernameForInboundFollowObject returns the local username when an IRI on this host refers to a
// configured local actor, for federation compatibility:
//
//   - Canonical profile: https://domain/@username (what we publish in actor JSON and WebFinger)
//   - Alias profile:     https://domain/users/username (common third-party / tooling convention)
//
// Use for inbound Follow.object, outbound follow resolution, and addressing (via RefAddressesLocalRecipient).
func (c *Config) LocalUsernameForInboundFollowObject(objectIRI string) (string, bool) {
	if u, ok := c.LocalUsernameForActorURL(objectIRI); ok {
		return u, true
	}
	objectIRI = strings.TrimSpace(objectIRI)
	if objectIRI == "" || c.PublicBaseURL == "" {
		return "", false
	}
	obj, err := url.Parse(objectIRI)
	if err != nil || obj.Host == "" {
		return "", false
	}
	base, err := url.Parse(c.PublicBaseURL)
	if err != nil || base.Hostname() == "" {
		return "", false
	}
	if !strings.EqualFold(obj.Hostname(), base.Hostname()) {
		return "", false
	}
	path := strings.Trim(obj.Path, "/")
	if path == "" {
		return "", false
	}
	if strings.HasPrefix(path, "@") {
		rest := strings.TrimPrefix(path, "@")
		handle, _, _ := strings.Cut(rest, "/")
		handle, err = url.PathUnescape(handle)
		if err != nil {
			return "", false
		}
		if handle != "" && c.IsLocalUsername(handle) {
			return handle, true
		}
		return "", false
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && parts[0] == "users" {
		handle, err := url.PathUnescape(parts[1])
		if err != nil {
			return "", false
		}
		if handle != "" && c.IsLocalUsername(handle) {
			return handle, true
		}
	}
	return "", false
}

// RefAddressesLocalRecipient reports whether ref is an IRI that targets this instance for delivery —
// a local user's profile (see LocalUsernameForInboundFollowObject) or the instance actor document.
// Used when scanning to/cc/bto/bcc/audience.
func (c *Config) RefAddressesLocalRecipient(ref string) bool {
	if _, ok := c.LocalUsernameForInboundFollowObject(ref); ok {
		return true
	}
	if c.PublicBaseURL == "" {
		return false
	}
	inst := strings.TrimSpace(c.InstanceActorIRI())
	if inst == "" {
		return false
	}
	uInst, err1 := url.Parse(inst)
	uRef, err2 := url.Parse(strings.TrimSpace(ref))
	if err1 == nil && err2 == nil && uInst.Host != "" && uRef.Host != "" &&
		strings.EqualFold(uInst.Hostname(), uRef.Hostname()) &&
		strings.TrimRight(uInst.Path, "/") == strings.TrimRight(uRef.Path, "/") {
		return true
	}
	return false
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
