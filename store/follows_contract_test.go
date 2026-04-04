package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
)

func TestIntegration_UpsertFollow_andAccept_andUndoDelete(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateAll(t, pool)

	cfg := &config.Config{PublicBaseURL: "https://f.test", LocalUsernames: []string{"a", "b"}, LocalUsername: "a"}
	aid, err := EnsureLocalActor(ctx, pool, cfg, "a", "k")
	if err != nil {
		t.Fatal(err)
	}
	bid, err := EnsureLocalActor(ctx, pool, cfg, "b", "k")
	if err != nil {
		t.Fatal(err)
	}
	followAct := "https://f.test/activities/f1"
	if err := UpsertFollow(ctx, pool, aid, bid, followAct, FollowStatePendingRemote); err != nil {
		t.Fatal(err)
	}
	st, err := GetFollowState(ctx, pool, aid, bid)
	if err != nil {
		t.Fatal(err)
	}
	if st != FollowStatePendingRemote {
		t.Fatalf("state %s", st)
	}
	if err := SetFollowStateByFollowActivityID(ctx, pool, followAct, FollowStateAccepted); err != nil {
		t.Fatal(err)
	}
	st, err = GetFollowState(ctx, pool, aid, bid)
	if err != nil {
		t.Fatal(err)
	}
	if st != FollowStateAccepted {
		t.Fatalf("after accept: %s", st)
	}
	if err := DeleteFollowByFollowActivityID(ctx, pool, followAct); err != nil {
		t.Fatal(err)
	}
	if _, err := GetFollowState(ctx, pool, aid, bid); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

func TestIntegration_SetFollowState_idempotentOnUnknownActivity(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateAll(t, pool)

	if err := SetFollowStateByFollowActivityID(ctx, pool, "https://none/nope", FollowStateAccepted); err != nil {
		t.Fatal(err)
	}
}
