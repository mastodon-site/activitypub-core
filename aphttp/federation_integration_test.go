package aphttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store"
)

func truncateFederationTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE TABLE queue_jobs, deliveries, follows, activities, objects, actors RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestIntegration_inboxPersistsActivityAndEnqueuesJob(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateFederationTables(t, pool)

	fix := newActorFixture(t)
	rec := &recordingQueue{}
	cfg := &config.Config{
		PublicBaseURL: "https://integration.test",
		LocalUsername: "localuser",
		InboxMaxBody:  1 << 20,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	actorBase := strings.TrimSuffix(fix.KeyID, "#main-key")
	actID := "https://remote.test/activities/post-one"
	body := mustJSON(t, map[string]any{"type": "Create", "id": actID, "actor": actorBase})
	req := mustSignedPost(t, "https://integration.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	var dbCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM activities WHERE activity_id = $1`, actID).Scan(&dbCount); err != nil {
		t.Fatal(err)
	}
	if dbCount != 1 {
		t.Fatalf("activities count = %d", dbCount)
	}

	jobs := rec.snapshotJobs()
	if len(jobs) != 1 {
		t.Fatalf("enqueued jobs: %d", len(jobs))
	}
	if jobs[0].Type != queue.TypeProcessInboxActivity {
		t.Fatalf("job type %q", jobs[0].Type)
	}
	if jobs[0].IdempotencyKey != actID {
		t.Fatalf("idempotency key %q", jobs[0].IdempotencyKey)
	}
	var payload map[string]int64
	if err := json.Unmarshal(jobs[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["activityDbId"] < 1 {
		t.Fatalf("payload %v", payload)
	}

	// Duplicate delivery: same activity id should not insert or enqueue again.
	rr2 := httptest.NewRecorder()
	h.SharedInbox(rr2, mustSignedPost(t, "https://integration.test/inbox", body, fix.KeyID, fix.Priv))
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("duplicate status %d", rr2.Code)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM activities WHERE activity_id = $1`, actID).Scan(&dbCount); err != nil {
		t.Fatal(err)
	}
	if dbCount != 1 {
		t.Fatalf("after duplicate, activities count = %d", dbCount)
	}
	if n := len(rec.snapshotJobs()); n != 1 {
		t.Fatalf("after duplicate, jobs = %d", n)
	}
}

func TestIntegration_inboxSkipsPersistenceWhenQueueNotConfigured(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateFederationTables(t, pool)

	fix := newActorFixture(t)
	cfg := &config.Config{
		PublicBaseURL: "https://integration.test",
		LocalUsername: "solo",
		InboxMaxBody:  1 << 20,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: nil})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	actorBase := strings.TrimSuffix(fix.KeyID, "#main-key")
	actID := "https://remote.test/activities/no-queue"
	body := mustJSON(t, map[string]any{"type": "Create", "id": actID, "actor": actorBase})
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, mustSignedPost(t, "https://integration.test/inbox", body, fix.KeyID, fix.Priv))
	if rr.Code != http.StatusAccepted {
		t.Fatal(rr.Code)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM activities WHERE activity_id = $1`, actID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected no row without queue, got %d", n)
	}
}

func TestIntegration_inboxSqlQueueInsertsPendingJobRow(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateFederationTables(t, pool)

	fix := newActorFixture(t)
	qb := sqlqueue.New(pool)
	cfg := &config.Config{
		PublicBaseURL: "https://integration.test",
		LocalUsername: "svc",
		InboxMaxBody:  1 << 20,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: qb})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	actorBase := strings.TrimSuffix(fix.KeyID, "#main-key")
	actID := "https://remote.test/activities/sqlq-1"
	body := mustJSON(t, map[string]any{"type": "Announce", "id": actID, "actor": actorBase})
	req := mustSignedPost(t, "https://integration.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM queue_jobs WHERE job_type = $1 AND status = 'pending'`, string(queue.TypeProcessInboxActivity)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pending inbox jobs: %d", n)
	}
}

func TestIntegration_outboxOrderedCollectionJSONShape(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateFederationTables(t, pool)

	rec := &recordingQueue{}
	cfg := &config.Config{
		PublicBaseURL: "https://integration.test",
		LocalUsername: "localuser",
		InboxMaxBody:  65536,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	localID := h.localActorIDs["localuser"]
	if localID == 0 {
		t.Fatal("local actor id")
	}

	_, err = pool.Exec(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Create','{}')`, "https://integration.test/o/first", localID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Create','{}')`, "https://integration.test/o/second", localID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h.Mount(mux)

	t.Run("activity_json_accept", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://integration.test/outbox/localuser", nil)
		req.Header.Set("Accept", "application/activity+json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d %s", rr.Code, rr.Body.String())
		}
		if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/activity+json") {
			t.Fatalf("content-type %q", ct)
		}
		doc := mustParseOutboxDoc(t, rr.Body.Bytes())
		if doc["@context"] != "https://www.w3.org/ns/activitystreams" {
			t.Fatalf("@context %#v", doc["@context"])
		}
		if doc["type"] != "OrderedCollection" {
			t.Fatalf("type %#v", doc["type"])
		}
		if doc["id"] != "https://integration.test/outbox/localuser" {
			t.Fatalf("id %#v", doc["id"])
		}
		if tot, ok := doc["totalItems"].(float64); !ok || int64(tot) != 2 {
			t.Fatalf("totalItems %#v", doc["totalItems"])
		}
		rawItems, ok := doc["orderedItems"].([]any)
		if !ok || len(rawItems) != 2 {
			t.Fatalf("orderedItems %#v", doc["orderedItems"])
		}
		if rawItems[0] != "https://integration.test/o/second" || rawItems[1] != "https://integration.test/o/first" {
			t.Fatalf("order %v", rawItems)
		}
	})

	t.Run("ld_json_default", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://integration.test/outbox/localuser", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatal(rr.Code)
		}
		ct := rr.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/ld+json") || !strings.Contains(ct, "profile=") {
			t.Fatalf("content-type %q", ct)
		}
	})

	t.Run("wrong_username_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://integration.test/outbox/nope", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status %d", rr.Code)
		}
	})

	t.Run("no_store_returns_503", func(t *testing.T) {
		hBare, err := New(cfg, Deps{})
		if err != nil {
			t.Fatal(err)
		}
		muxBare := http.NewServeMux()
		hBare.Mount(muxBare)
		req := httptest.NewRequest(http.MethodGet, "https://integration.test/outbox/localuser", nil)
		rr := httptest.NewRecorder()
		muxBare.ServeHTTP(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", rr.Code)
		}
	})
}
