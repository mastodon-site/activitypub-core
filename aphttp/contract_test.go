package aphttp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/queue"
)

// applyTestingFetchPolicy relaxes outbound fetch for httptest/localhost (tests only).
func applyTestingFetchPolicy(h *Handler) {
	pol := fetch.TestingPolicy()
	h.fetchPolicy = pol
	h.fetchClient = fetch.NewHTTPClientForPolicy(pol, 30*time.Second)
}

// recordingQueue satisfies queue.Backend for contract tests (inbox enqueue path only).
type recordingQueue struct {
	mu   sync.Mutex
	jobs []queue.Job
}

func (r *recordingQueue) Enqueue(ctx context.Context, job queue.Job) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, job)
	return nil
}

func (r *recordingQueue) Dequeue(ctx context.Context) (*queue.Lease, error) {
	_ = ctx
	return nil, nil
}

func (r *recordingQueue) Ack(ctx context.Context, id int64) error {
	_, _ = ctx, id
	return nil
}

func (r *recordingQueue) Nack(ctx context.Context, id int64, requeue bool) error {
	_, _, _ = ctx, id, requeue
	return nil
}

func (r *recordingQueue) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = nil
}

func (r *recordingQueue) snapshotJobs() []queue.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]queue.Job, len(r.jobs))
	copy(out, r.jobs)
	return out
}

// actorFixture serves a minimal ActivityPub actor document for HTTP Signature tests.
type actorFixture struct {
	KeyID  string
	Priv   *rsa.PrivateKey
	Client *http.Client
}

func newActorFixture(t *testing.T) *actorFixture {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr, err := actorkey.PublicKeyPEMFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		kid := base + "/users/remote#main-key"
		actor := map[string]any{
			"@context": []string{"https://www.w3.org/ns/activitystreams"},
			"id":       base + "/users/remote",
			"type":     "Person",
			"publicKey": map[string]any{
				"id":           kid,
				"owner":        base + "/users/remote",
				"type":         "Key",
				"publicKeyPem": pemStr,
			},
		}
		j, err := json.Marshal(actor)
		if err != nil {
			t.Error(err)
			http.Error(w, "err", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(j)
	}))
	t.Cleanup(srv.Close)
	keyID := srv.URL + "/users/remote#main-key"

	return &actorFixture{
		KeyID:  keyID,
		Priv:   priv,
		Client: srv.Client(),
	}
}

func mustParseOutboxDoc(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("outbox json: %v", err)
	}
	return m
}

// httptestNewRequestAbsolute builds a server-bound GET from an absolute URL (path + query + Host).
// Using httptest.NewRequest(method, absURL, nil) alone can leave Request.Host empty on some Go versions,
// which breaks handlers that rely on the request URL matching the public site.
func httptestNewRequestAbsolute(t *testing.T, method, abs string) *http.Request {
	t.Helper()
	u, err := url.Parse(abs)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	uri := u.RequestURI()
	if uri == "" {
		t.Fatalf("empty request-uri for %q", abs)
	}
	r := httptest.NewRequest(method, uri, nil)
	r.Host = u.Host
	return r
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "db", "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				t.Fatal(err)
			}
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find db/migrations (run tests from module root or aphttp/)")
	return ""
}

func integrationDatabaseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("AP_TEST_DATABASE_URL")
	if u == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AP_TEST_DATABASE_URL is required in CI")
		}
		t.Skip("set AP_TEST_DATABASE_URL for PostgreSQL integration contract tests")
	}
	return u
}
