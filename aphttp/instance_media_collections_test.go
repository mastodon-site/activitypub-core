package aphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	fsblob "github.com/mastodon-site/activitypub-core/blobs/fs"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/store"
)

func TestContract_GetInstanceActor_and_actorRedirect(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "u"}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	t.Run("well_known", func(t *testing.T) {
		rr := httptest.NewRecorder()
		th.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "https://inst.test/.well-known/actor", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d", rr.Code)
		}
		if !bytes.Contains(rr.Body.Bytes(), []byte(`"id":"https://inst.test/.well-known/actor"`)) {
			t.Fatalf("body %s", rr.Body.String())
		}
	})

	t.Run("actor_alias_redirect", func(t *testing.T) {
		rr := httptest.NewRecorder()
		th.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "https://inst.test/actor", nil))
		if rr.Code != http.StatusPermanentRedirect {
			t.Fatalf("status %d", rr.Code)
		}
		if loc := rr.Header().Get("Location"); loc != "https://inst.test/.well-known/actor" {
			t.Fatalf("location %q", loc)
		}
	})
}

func TestContract_mediaUpload_requiresSecret(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL: "https://m.test",
		LocalUsername: "u",
	}
	blob := fsblob.New(t.TempDir())
	h, err := New(cfg, Deps{Blobs: blob})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "https://m.test/media", bytes.NewReader([]byte("x"))))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestContract_mediaUpload_roundTrip(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:       "https://m.test",
		LocalUsername:       "u",
		MediaUploadSecret:   "secretsecretsecretsecretsecret12",
		MediaMaxUploadBytes: 1 << 20,
	}
	blob := fsblob.New(t.TempDir())
	h, err := New(cfg, Deps{Blobs: blob})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	req := httptest.NewRequest(http.MethodPost, "https://m.test/media", bytes.NewReader([]byte("hello")))
	req.Header.Set("Authorization", "Bearer secretsecretsecretsecretsecret12")
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("post %d %s", rr.Code, rr.Body.String())
	}
	var up struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &up); err != nil {
		t.Fatal(err)
	}
	if up.URL == "" {
		t.Fatal("empty url")
	}

	t.Run("get_blob", func(t *testing.T) {
		rr2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodGet, up.URL, nil)
		th.ServeHTTP(rr2, req2)
		if rr2.Code != http.StatusOK || rr2.Body.String() != "hello" {
			t.Fatalf("get %d %q", rr2.Code, rr2.Body.String())
		}
	})

	t.Run("reject_unsafe_key", func(t *testing.T) {
		rr2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodGet, "https://m.test/media/bad*key", nil)
		th.ServeHTTP(rr2, req2)
		if rr2.Code != http.StatusNotFound {
			t.Fatalf("unsafe key got %d", rr2.Code)
		}
	})
}

func TestIntegration_followersCollection_includesAcceptedActor(t *testing.T) {
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

	cfg := &config.Config{
		PublicBaseURL:  "https://fc.test",
		LocalUsernames: []string{"alice", "bob"},
		LocalUsername:  "alice",
		InboxMaxBody:   4096,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: nil})
	if err != nil {
		t.Fatal(err)
	}
	aliceID := h.localActorIDs["alice"]
	bobID := h.localActorIDs["bob"]
	if err := store.UpsertFollow(ctx, pool, aliceID, bobID, "https://fc.test/a/f1", store.FollowStateAccepted); err != nil {
		t.Fatal(err)
	}

	th := testMounted(h)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://fc.test/@bob/followers", nil)
	req.Header.Set("Accept", "application/activity+json")
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(cfg.LocalActorProfileURL("alice"))) {
		t.Fatalf("expected alice IRI in followers: %s", rr.Body.String())
	}
}
