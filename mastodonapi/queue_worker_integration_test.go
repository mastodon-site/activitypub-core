package mastodonapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
	"github.com/mastodon-site/activitypub-core/internal/worker"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

// TestIntegration_Post_sqlQueue_timelineAndDeliverWorker exercises the SQL-backed queue used in
// production: a public post is persisted (visible on timelines immediately), a deliver_activity job
// is stored for an accepted remote follower, and apw-style processing delivers a signed POST to the stub inbox.
func TestIntegration_Post_sqlQueue_timelineAndDeliverWorker(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	st, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	truncateMastodonTestDB(t, st.Pool)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "actor.pem")
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	const remoteBob = "https://fanout.test/users/bob"
	var inboxPosts atomic.Int32
	inboxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		if err := httpsig.VerifyRequest(r, body, &priv.PublicKey); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		inboxPosts.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(inboxSrv.Close)
	inboxURL := inboxSrv.URL + "/inbox"

	publicBase := "https://queue-flow.test"
	cfg := &config.Config{
		PublicBaseURL:       publicBase,
		LocalUsername:       "alice",
		LocalUsernames:      []string{"alice"},
		ActorPrivateKeyPath: keyPath,
		FetchRelaxLocal:     true, // delivery to httptest loopback + http
	}
	// Inbox must look like an AP inbox path so InboxURLFromReference stops after reading the actor doc.
	fetchClient := mastodonIntegrationStubFetchClientWithRemote(publicBase, remoteBob, inboxURL)

	h, err := aphttp.New(cfg, aphttp.Deps{
		Store:       st,
		Queue:       sqlqueue.New(st.Pool),
		FetchPolicy: fetch.TestingPolicy(),
		FetchClient: fetchClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := &Server{H: h, Pool: st.Pool}
	srv.mountMastodon(mux)

	aliceID, ok := h.LocalActorID("alice")
	if !ok || aliceID < 1 {
		t.Fatalf("alice id=%d ok=%v", aliceID, ok)
	}
	rid, err := store.EnsureRemoteActor(ctx, st.Pool, remoteBob, "(integration-remote)")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertFollow(ctx, st.Pool, rid, aliceID, remoteBob+"#follow/1", store.FollowStateAccepted); err != nil {
		t.Fatal(err)
	}

	app, err := store.InsertOAuthApplication(ctx, st.Pool, "qflow", "https://cb/q", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	const rawTok = "queue-flow-token"
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(rawTok), app.ID, aliceID); err != nil {
		t.Fatal(err)
	}

	body := `{"status":"queued delivery fan-out"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawTok)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post %d %s", rec.Code, rec.Body.String())
	}
	var posted map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &posted); err != nil {
		t.Fatal(err)
	}
	statusID := fmt.Sprint(posted["id"])

	recPub := httptest.NewRecorder()
	mux.ServeHTTP(recPub, httptest.NewRequest(http.MethodGet, "/api/v1/timelines/public", nil))
	if recPub.Code != http.StatusOK {
		t.Fatalf("public timeline %d %s", recPub.Code, recPub.Body.String())
	}
	var pubList []map[string]any
	if err := json.Unmarshal(recPub.Body.Bytes(), &pubList); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range pubList {
		if fmt.Sprint(row["id"]) == statusID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("status %s not on public timeline: %s", statusID, recPub.Body.String())
	}

	var pendingDelivers int64
	if err := st.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM queue_jobs
		WHERE status = 'pending' AND job_type = 'deliver_activity'`).Scan(&pendingDelivers); err != nil {
		t.Fatal(err)
	}
	if pendingDelivers < 1 {
		t.Fatalf("expected pending deliver_activity jobs, got %d", pendingDelivers)
	}

	qb := sqlqueue.New(st.Pool)
	var lease *queue.Lease
	for attempt := 0; attempt < 5; attempt++ {
		l, err := qb.Dequeue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if l == nil {
			t.Fatal("dequeue returned nil with pending jobs")
		}
		if l.Type == queue.TypeDeliverActivity {
			lease = l
			break
		}
		if err := worker.ProcessLease(ctx, cfg, st.Pool, qb, l, nil); err != nil {
			t.Fatalf("unexpected job type %s process: %v", l.Type, err)
		}
		if err := qb.Ack(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
	}
	if lease == nil {
		t.Fatal("no deliver_activity lease found")
	}
	if err := worker.ProcessLease(ctx, cfg, st.Pool, qb, lease, nil); err != nil {
		t.Fatal(err)
	}
	if err := qb.Ack(ctx, lease.ID); err != nil {
		t.Fatal(err)
	}
	if inboxPosts.Load() != 1 {
		t.Fatalf("inbox POST count = %d, want 1", inboxPosts.Load())
	}
}

func TestIntegration_sqlQueue_noopDequeueProcessed(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	st, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	truncateMastodonTestDB(t, st.Pool)

	qb := sqlqueue.New(st.Pool)
	if err := qb.Enqueue(ctx, queue.Job{Type: queue.TypeNoop, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	lease, err := qb.Dequeue(ctx)
	if err != nil || lease == nil {
		t.Fatalf("dequeue: %v lease=%v", err, lease)
	}
	cfg := &config.Config{PublicBaseURL: "https://noop.test", LocalUsername: "alice"}
	if err := worker.ProcessLease(ctx, cfg, st.Pool, qb, lease, nil); err != nil {
		t.Fatal(err)
	}
	if err := qb.Ack(ctx, lease.ID); err != nil {
		t.Fatal(err)
	}
	var left int64
	if err := st.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM queue_jobs WHERE status = 'done'`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("done rows: %d", left)
	}
}
