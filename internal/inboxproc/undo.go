package inboxproc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/as2"
	"github.com/mastodon-site/activitypub-core/store"
)

func handleUndo(ctx context.Context, pool *pgxpool.Pool, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	raw, ok := fields["object"]
	if !ok {
		return fmt.Errorf("undo missing object")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return undoBareActivityIRI(ctx, pool, row.ActorID, s)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("undo object: %w", err)
	}
	id, err := jsonStringFromMap(obj, "id")
	if err != nil {
		return err
	}
	return deleteUndoTarget(ctx, pool, row.ActorID, obj, id)
}

func undoBareActivityIRI(ctx context.Context, pool *pgxpool.Pool, actorID int64, activityIRI string) error {
	tag, err := pool.Exec(ctx, `
		DELETE FROM follows WHERE follow_activity_id = $1 AND follower_actor_id = $2
	`, activityIRI, actorID)
	if err != nil {
		return fmt.Errorf("undo follow: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag, err = pool.Exec(ctx, `
		DELETE FROM federated_likes WHERE like_activity_id = $1 AND actor_id = $2
	`, activityIRI, actorID)
	if err != nil {
		return fmt.Errorf("undo like: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag, err = pool.Exec(ctx, `
		DELETE FROM federated_announces WHERE announce_activity_id = $1 AND actor_id = $2
	`, activityIRI, actorID)
	if err != nil {
		return fmt.Errorf("undo announce: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag, err = pool.Exec(ctx, `
		DELETE FROM federated_blocks WHERE block_activity_id = $1 AND blocker_actor_id = $2
	`, activityIRI, actorID)
	if err != nil {
		return fmt.Errorf("undo block: %w", err)
	}
	return nil
}

func jsonStringFromMap(m map[string]json.RawMessage, key string) (string, error) {
	raw, ok := m[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return "", fmt.Errorf("invalid %s", key)
	}
	return s, nil
}

func deleteUndoTarget(ctx context.Context, pool *pgxpool.Pool, actorID int64, obj map[string]json.RawMessage, id string) error {
	t := undoTargetType(obj)
	switch {
	case strings.EqualFold(t, "Follow"):
		return store.DeleteFollowByFollowActivityIDForFollower(ctx, pool, id, actorID)
	case strings.EqualFold(t, "Like"):
		return store.DeleteFederatedLikeByActivityID(ctx, pool, id, actorID)
	case strings.EqualFold(t, "Announce"):
		return store.DeleteFederatedAnnounceByActivityID(ctx, pool, id, actorID)
	case strings.EqualFold(t, "Block"):
		return store.DeleteFederatedBlockByActivityID(ctx, pool, id, actorID)
	case strings.EqualFold(t, "Accept") || strings.EqualFold(t, "Reject"):
		// Remote undo of Accept/Reject is unusual; no local edge keyed by Accept id.
		return nil
	default:
		return nil
	}
}

func undoTargetType(obj map[string]json.RawMessage) string {
	raw, ok := obj["type"]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return as2.NormalizeActivityType(s)
	}
	var arr []any
	if json.Unmarshal(raw, &arr) == nil {
		for _, el := range arr {
			if s, ok := el.(string); ok {
				return as2.NormalizeActivityType(s)
			}
		}
	}
	return ""
}
