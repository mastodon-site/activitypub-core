package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
)

func TestIntegration_ResolveLocalFolloweeFromObjectIRI(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	dsn := testDatabaseURL(t)
	ctx := context.Background()
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateAll(t, pool)

	cfg := &config.Config{
		PublicBaseURL:  "https://resolve.test",
		LocalUsernames: []string{"admin", "other"},
		LocalUsername:  "admin",
	}
	if _, err := UpsertLocalActor(ctx, pool, cfg, "admin", "pem"); err != nil {
		t.Fatal(err)
	}

	t.Run("exact_actor_url", func(t *testing.T) {
		id, u, err := ResolveLocalFolloweeFromObjectIRI(ctx, pool, cfg, cfg.LocalActorProfileURL("admin"))
		if err != nil || u != "admin" || id < 1 {
			t.Fatalf("id=%d u=%q err=%v", id, u, err)
		}
	})

	t.Run("users_path_case_insensitive", func(t *testing.T) {
		id, u, err := ResolveLocalFolloweeFromObjectIRI(ctx, pool, cfg, "https://resolve.test/users/Admin")
		if err != nil || u != "admin" || id < 1 {
			t.Fatalf("id=%d u=%q err=%v", id, u, err)
		}
	})

	t.Run("at_path_case_insensitive", func(t *testing.T) {
		id, u, err := ResolveLocalFolloweeFromObjectIRI(ctx, pool, cfg, "https://RESOLVE.TEST/@ADMIN")
		if err != nil || u != "admin" || id < 1 {
			t.Fatalf("id=%d u=%q err=%v", id, u, err)
		}
	})

	t.Run("db_user_without_config_username_still_resolves", func(t *testing.T) {
		if _, err := UpsertLocalActor(ctx, pool, cfg, "bootstrap", "pem"); err != nil {
			t.Fatal(err)
		}
		narrow := &config.Config{
			PublicBaseURL:  "https://resolve.test",
			LocalUsernames: []string{"other"},
			LocalUsername:  "other",
		}
		id, u, err := ResolveLocalFolloweeFromObjectIRI(ctx, pool, narrow, "https://resolve.test/users/bootstrap")
		if err != nil || u != "bootstrap" {
			t.Fatalf("id=%d u=%q err=%v", id, u, err)
		}
	})

	t.Run("foreign_host_rejected", func(t *testing.T) {
		_, _, err := ResolveLocalFolloweeFromObjectIRI(ctx, pool, cfg, "https://elsewhere.test/users/admin")
		if err == nil || !strings.Contains(err.Error(), "not a local actor") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("followers_collection_rejected", func(t *testing.T) {
		_, _, err := ResolveLocalFolloweeFromObjectIRI(ctx, pool, cfg, cfg.LocalActorFollowersURL("admin"))
		if err == nil || !strings.Contains(err.Error(), "collection") {
			t.Fatalf("err=%v", err)
		}
	})
}
