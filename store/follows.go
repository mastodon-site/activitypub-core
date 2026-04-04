package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Follow relationship states.
const (
	FollowStatePending       = "pending"        // inbound Follow stored; Accept not yet sent (or manual)
	FollowStateAccepted      = "accepted"       // mutual agreement
	FollowStateRejected      = "rejected"       // follow denied
	FollowStatePendingRemote = "pending_remote" // outbound Follow delivered; awaiting remote Accept
	FollowStateUndone        = "undone"         // relationship removed (Undo)
)

type dbExec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// UpsertFollow stores or updates a follow edge (same follower/followee pair).
func UpsertFollow(ctx context.Context, q dbExec, followerID, followeeID int64, followActivityID, state string) error {
	if followActivityID == "" {
		return fmt.Errorf("follow activity id required")
	}
	_, err := q.Exec(ctx, `
		INSERT INTO follows (follower_actor_id, followee_actor_id, state, follow_activity_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (follower_actor_id, followee_actor_id) DO UPDATE SET
			state = EXCLUDED.state,
			follow_activity_id = EXCLUDED.follow_activity_id,
			updated_at = now()
	`, followerID, followeeID, state, followActivityID)
	return err
}

// SetFollowStateByFollowActivityID updates state for the row keyed by the original Follow activity IRI.
func SetFollowStateByFollowActivityID(ctx context.Context, q dbExec, followActivityID, state string) error {
	_, err := q.Exec(ctx, `UPDATE follows SET state = $2, updated_at = now() WHERE follow_activity_id = $1`, followActivityID, state)
	return err
}

// SetFollowStateByFollowActivityIDForFollower updates follow state only when follower_actor_id matches.
// Used for federated Accept/Reject so a remote actor cannot mutate another actor's follow edge.
func SetFollowStateByFollowActivityIDForFollower(ctx context.Context, q dbExec, followActivityID, state string, followerActorID int64) error {
	_, err := q.Exec(ctx, `
		UPDATE follows SET state = $2, updated_at = now()
		WHERE follow_activity_id = $1 AND follower_actor_id = $3
	`, followActivityID, state, followerActorID)
	return err
}

// DeleteFollowByPair removes a follow relationship (unfollow / Undo).
func DeleteFollowByPair(ctx context.Context, q dbExec, followerID, followeeID int64) error {
	_, err := q.Exec(ctx, `DELETE FROM follows WHERE follower_actor_id = $1 AND followee_actor_id = $2`, followerID, followeeID)
	return err
}

// DeleteFollowByFollowActivityID removes the edge created by that Follow activity (Undo).
func DeleteFollowByFollowActivityID(ctx context.Context, q dbExec, followActivityIRI string) error {
	_, err := q.Exec(ctx, `DELETE FROM follows WHERE follow_activity_id = $1`, followActivityIRI)
	return err
}

// DeleteFollowByFollowActivityIDForFollower deletes by follow activity IRI only when the follower matches.
func DeleteFollowByFollowActivityIDForFollower(ctx context.Context, q dbExec, followActivityIRI string, followerActorID int64) error {
	_, err := q.Exec(ctx, `DELETE FROM follows WHERE follow_activity_id = $1 AND follower_actor_id = $2`, followActivityIRI, followerActorID)
	return err
}

// FollowExistsBetween reports whether an accepted (or pending_remote) follow exists.
func FollowExistsBetween(ctx context.Context, pool *pgxpool.Pool, followerID, followeeID int64) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM follows
		WHERE follower_actor_id = $1 AND followee_actor_id = $2
		  AND state IN ('accepted', 'pending', 'pending_remote')
	`, followerID, followeeID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetFollowState returns state for the pair, or empty if none.
func GetFollowState(ctx context.Context, pool *pgxpool.Pool, followerID, followeeID int64) (string, error) {
	var st string
	err := pool.QueryRow(ctx, `
		SELECT state FROM follows WHERE follower_actor_id = $1 AND followee_actor_id = $2
	`, followerID, followeeID).Scan(&st)
	if err != nil {
		return "", err
	}
	return st, nil
}
