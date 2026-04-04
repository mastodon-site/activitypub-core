package store

import (
	"context"
	"fmt"
)

// UpsertFederatedLike records a Like activity; one like per actor per object (latest activity id wins).
func UpsertFederatedLike(ctx context.Context, q dbExec, actorID int64, objectURL, likeActivityID string) error {
	if objectURL == "" || likeActivityID == "" {
		return fmt.Errorf("like object url and activity id required")
	}
	_, err := q.Exec(ctx, `
		INSERT INTO federated_likes (actor_id, object_url, like_activity_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (actor_id, object_url) DO UPDATE SET
			like_activity_id = EXCLUDED.like_activity_id,
			created_at = now()
	`, actorID, objectURL, likeActivityID)
	return err
}

// DeleteFederatedLikeByActivityID removes a like when the actor matches (Undo).
func DeleteFederatedLikeByActivityID(ctx context.Context, q dbExec, likeActivityID string, likerActorID int64) error {
	_, err := q.Exec(ctx, `
		DELETE FROM federated_likes WHERE like_activity_id = $1 AND actor_id = $2
	`, likeActivityID, likerActorID)
	return err
}

// UpsertFederatedAnnounce records an Announce (boost).
func UpsertFederatedAnnounce(ctx context.Context, q dbExec, actorID int64, objectURL, announceActivityID string) error {
	if objectURL == "" || announceActivityID == "" {
		return fmt.Errorf("announce object url and activity id required")
	}
	_, err := q.Exec(ctx, `
		INSERT INTO federated_announces (actor_id, object_url, announce_activity_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (actor_id, object_url) DO UPDATE SET
			announce_activity_id = EXCLUDED.announce_activity_id,
			created_at = now()
	`, actorID, objectURL, announceActivityID)
	return err
}

// DeleteFederatedAnnounceByActivityID removes a boost when the actor matches (Undo).
func DeleteFederatedAnnounceByActivityID(ctx context.Context, q dbExec, announceActivityID string, announcerActorID int64) error {
	_, err := q.Exec(ctx, `
		DELETE FROM federated_announces WHERE announce_activity_id = $1 AND actor_id = $2
	`, announceActivityID, announcerActorID)
	return err
}

// UpsertFederatedBlock records a Block activity edge.
func UpsertFederatedBlock(ctx context.Context, q dbExec, blockerActorID int64, blockedActorURL, blockActivityID string) error {
	if blockedActorURL == "" || blockActivityID == "" {
		return fmt.Errorf("block target and activity id required")
	}
	_, err := q.Exec(ctx, `
		INSERT INTO federated_blocks (blocker_actor_id, blocked_actor_url, block_activity_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (blocker_actor_id, blocked_actor_url) DO UPDATE SET
			block_activity_id = EXCLUDED.block_activity_id,
			created_at = now()
	`, blockerActorID, CanonicalActorURL(blockedActorURL), blockActivityID)
	return err
}

// DeleteFederatedBlockByActivityID removes a block by the original Block activity id.
func DeleteFederatedBlockByActivityID(ctx context.Context, q dbExec, blockActivityID string, blockerActorID int64) error {
	_, err := q.Exec(ctx, `
		DELETE FROM federated_blocks WHERE block_activity_id = $1 AND blocker_actor_id = $2
	`, blockActivityID, blockerActorID)
	return err
}
