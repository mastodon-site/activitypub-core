package aphttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/store"
)

func TestCanonicalIRICandidates(t *testing.T) {
	c := canonicalIRICandidates("https://ex.test", "/o/1")
	if len(c) != 2 {
		t.Fatalf("got %d: %v", len(c), c)
	}
	if c[0] != "https://ex.test/o/1" || c[1] != "https://ex.test/o/1/" {
		t.Fatalf("unexpected %v", c)
	}
	c = canonicalIRICandidates("https://ex.test/", "/p/")
	if c[0] != "https://ex.test/p/" || c[1] != "https://ex.test/p" {
		t.Fatalf("got %v", c)
	}
}

func TestContract_GETActivityOrObject_servesLocalActivity(t *testing.T) {
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
	_, err = pool.Exec(ctx, `TRUNCATE TABLE queue_jobs, deliveries, follows, federated_likes, federated_announces, federated_blocks, activities, objects, actors RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		PublicBaseURL:  "https://getobj.test",
		LocalUsernames: []string{"alice"},
		LocalUsername:  "alice",
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: nil})
	if err != nil {
		t.Fatal(err)
	}
	actID := "https://getobj.test/posts/p1"
	raw := []byte(`{"@context":"https://www.w3.org/ns/activitystreams","id":"` + actID + `","type":"Create","actor":"` + cfg.LocalActorProfileURL("alice") + `","object":{"id":"https://getobj.test/obj/n1","type":"Note","content":"hi"}}`)
	aliceDB := h.localActorIDs["alice"]
	if _, err := pool.Exec(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Create',$3::jsonb)`,
		actID, aliceDB, raw); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "https://getobj.test/posts/p1", nil)
	req.Header.Set("Accept", "application/activity+json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/activity+json; charset=utf-8" {
		t.Fatalf("ct %q", ct)
	}
}

func TestContract_GETActivityOrObject_servesLocalObject(t *testing.T) {
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
	_, err = pool.Exec(ctx, `TRUNCATE TABLE queue_jobs, deliveries, follows, federated_likes, federated_announces, federated_blocks, activities, objects, actors RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		PublicBaseURL:  "https://getobj2.test",
		LocalUsernames: []string{"bob"},
		LocalUsername:  "bob",
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: nil})
	if err != nil {
		t.Fatal(err)
	}
	objID := "https://getobj2.test/obj/note-aa"
	note := []byte(`{"id":"` + objID + `","type":"Note","content":"x","attributedTo":"` + cfg.LocalActorProfileURL("bob") + `"}`)
	bobDB := h.localActorIDs["bob"]
	if _, err := pool.Exec(ctx, `INSERT INTO objects (object_url, actor_id, type, raw_json) VALUES ($1,$2,'Note',$3::jsonb)`,
		objID, bobDB, note); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, objID, nil)
	req.Header.Set("Accept", "application/activity+json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_GETActivityOrObject_hidesRemoteOwnedActivity(t *testing.T) {
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
	_, err = pool.Exec(ctx, `TRUNCATE TABLE queue_jobs, deliveries, follows, federated_likes, federated_announces, federated_blocks, activities, objects, actors RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		PublicBaseURL:  "https://hide.test",
		LocalUsernames: []string{"local"},
		LocalUsername:  "local",
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: nil})
	if err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://evil.test/users/u", "pem")
	if err != nil {
		t.Fatal(err)
	}
	actID := "https://hide.test/crafted/path" // path on our host, activity attributed to remote via actor_id
	raw := []byte(`{"id":"` + actID + `","type":"Create"}`)
	if _, err := pool.Exec(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Create',$3::jsonb)`,
		actID, rid, raw); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, actID, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", rr.Code, rr.Body.String())
	}
}
