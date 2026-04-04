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

// Regression: federated Accept/Reject/Undo must only affect follows where
// follower_actor_id matches the remote actor making the change (CVE-class
// confused-deputy on follow_activity_id alone).
func TestSecurity_followStateUpdateRequiresMatchingFollower(t *testing.T) {
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

	cfg := &config.Config{PublicBaseURL: "https://sec-follow.test", LocalUsernames: []string{"local"}, LocalUsername: "local"}
	localID, err := EnsureLocalActor(ctx, pool, cfg, "local", "k")
	if err != nil {
		t.Fatal(err)
	}
	victimID, err := EnsureRemoteActor(ctx, pool, "https://victim.example/users/v", "pem-v")
	if err != nil {
		t.Fatal(err)
	}
	attackerID, err := EnsureRemoteActor(ctx, pool, "https://attacker.example/users/a", "pem-a")
	if err != nil {
		t.Fatal(err)
	}
	followIRI := "https://victim.example/activities/follow-sec-1"
	if err := UpsertFollow(ctx, pool, victimID, localID, followIRI, FollowStatePendingRemote); err != nil {
		t.Fatal(err)
	}

	if err := SetFollowStateByFollowActivityIDForFollower(ctx, pool, followIRI, FollowStateAccepted, attackerID); err != nil {
		t.Fatal(err)
	}
	st, err := GetFollowState(ctx, pool, victimID, localID)
	if err != nil {
		t.Fatal(err)
	}
	if st != FollowStatePendingRemote {
		t.Fatalf("wrong follower must not change state: got %q", st)
	}

	if err := SetFollowStateByFollowActivityIDForFollower(ctx, pool, followIRI, FollowStateAccepted, victimID); err != nil {
		t.Fatal(err)
	}
	st, err = GetFollowState(ctx, pool, victimID, localID)
	if err != nil {
		t.Fatal(err)
	}
	if st != FollowStateAccepted {
		t.Fatalf("victim's own accept update should work: got %q", st)
	}
}

func TestSecurity_followRejectRequiresMatchingFollower(t *testing.T) {
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

	cfg := &config.Config{PublicBaseURL: "https://sec-reject.test", LocalUsernames: []string{"local"}, LocalUsername: "local"}
	localID, err := EnsureLocalActor(ctx, pool, cfg, "local", "k")
	if err != nil {
		t.Fatal(err)
	}
	victimID, err := EnsureRemoteActor(ctx, pool, "https://victim2.example/users/v", "pem-v")
	if err != nil {
		t.Fatal(err)
	}
	attackerID, err := EnsureRemoteActor(ctx, pool, "https://attacker2.example/users/a", "pem-a")
	if err != nil {
		t.Fatal(err)
	}
	followIRI := "https://victim2.example/activities/follow-rej"
	if err := UpsertFollow(ctx, pool, victimID, localID, followIRI, FollowStatePendingRemote); err != nil {
		t.Fatal(err)
	}

	if err := SetFollowStateByFollowActivityIDForFollower(ctx, pool, followIRI, FollowStateRejected, attackerID); err != nil {
		t.Fatal(err)
	}
	st, err := GetFollowState(ctx, pool, victimID, localID)
	if err != nil {
		t.Fatal(err)
	}
	if st != FollowStatePendingRemote {
		t.Fatalf("wrong follower must not reject others' follow: got %q", st)
	}
}

func TestSecurity_followDeleteByActivityRequiresMatchingFollower(t *testing.T) {
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

	cfg := &config.Config{PublicBaseURL: "https://sec-del.test", LocalUsernames: []string{"local"}, LocalUsername: "local"}
	localID, err := EnsureLocalActor(ctx, pool, cfg, "local", "k")
	if err != nil {
		t.Fatal(err)
	}
	victimID, err := EnsureRemoteActor(ctx, pool, "https://victim3.example/users/v", "pem-v")
	if err != nil {
		t.Fatal(err)
	}
	attackerID, err := EnsureRemoteActor(ctx, pool, "https://attacker3.example/users/a", "pem-a")
	if err != nil {
		t.Fatal(err)
	}
	followIRI := "https://victim3.example/activities/follow-del"
	if err := UpsertFollow(ctx, pool, victimID, localID, followIRI, FollowStateAccepted); err != nil {
		t.Fatal(err)
	}

	if err := DeleteFollowByFollowActivityIDForFollower(ctx, pool, followIRI, attackerID); err != nil {
		t.Fatal(err)
	}
	st, err := GetFollowState(ctx, pool, victimID, localID)
	if err != nil {
		t.Fatal(err)
	}
	if st != FollowStateAccepted {
		t.Fatalf("wrong follower must not delete edge: got %q", st)
	}

	if err := DeleteFollowByFollowActivityIDForFollower(ctx, pool, followIRI, victimID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetFollowState(ctx, pool, victimID, localID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("victim delete should remove follow: %v", err)
	}
}

// Regression: POST /outbox/{user} Undo uses follower_actor_id = that user; another local user must not
// remove a follow edge owned by a different local actor (same pattern as remote ForFollower).
func TestSecurity_localActorCannotDeleteAnotherLocalsOutboundFollow(t *testing.T) {
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

	cfg := &config.Config{PublicBaseURL: "https://multi-local.test", LocalUsernames: []string{"alice", "bob"}, LocalUsername: "alice"}
	aliceLocal, err := EnsureLocalActor(ctx, pool, cfg, "alice", "k")
	if err != nil {
		t.Fatal(err)
	}
	bobLocal, err := EnsureLocalActor(ctx, pool, cfg, "bob", "k")
	if err != nil {
		t.Fatal(err)
	}
	remoteID, err := EnsureRemoteActor(ctx, pool, "https://remote-out.test/users/r", "pem")
	if err != nil {
		t.Fatal(err)
	}
	followIRI := "https://multi-local.test/activities/bob-follows-remote"
	if err := UpsertFollow(ctx, pool, bobLocal, remoteID, followIRI, FollowStatePendingRemote); err != nil {
		t.Fatal(err)
	}

	if err := DeleteFollowByFollowActivityIDForFollower(ctx, pool, followIRI, aliceLocal); err != nil {
		t.Fatal(err)
	}
	st, err := GetFollowState(ctx, pool, bobLocal, remoteID)
	if err != nil {
		t.Fatal(err)
	}
	if st != FollowStatePendingRemote {
		t.Fatalf("alice must not delete bob's follow: state %q", st)
	}
}
