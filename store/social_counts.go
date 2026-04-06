package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CountFederatedLikesOnObjectURL counts like rows for a Note id (IRI).
func CountFederatedLikesOnObjectURL(ctx context.Context, pool *pgxpool.Pool, objectURL string) (int64, error) {
	objectURL = CanonicalActorURL(objectURL)
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM federated_likes WHERE object_url = $1
	`, objectURL).Scan(&n)
	return n, err
}

// CountFederatedAnnouncesOnObjectURL counts announce (boost) rows for a Note id (IRI).
func CountFederatedAnnouncesOnObjectURL(ctx context.Context, pool *pgxpool.Pool, objectURL string) (int64, error) {
	objectURL = CanonicalActorURL(objectURL)
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM federated_announces WHERE object_url = $1
	`, objectURL).Scan(&n)
	return n, err
}

// FederatedLikeActivityID returns the stored Like activity IRI for this actor/object pair, if any.
func FederatedLikeActivityID(ctx context.Context, pool *pgxpool.Pool, actorID int64, objectURL string) (string, error) {
	objectURL = CanonicalActorURL(objectURL)
	var id string
	err := pool.QueryRow(ctx, `
		SELECT like_activity_id FROM federated_likes WHERE actor_id = $1 AND object_url = $2
	`, actorID, objectURL).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// FederatedAnnounceActivityID returns the stored Announce activity IRI for this actor/object pair, if any.
func FederatedAnnounceActivityID(ctx context.Context, pool *pgxpool.Pool, actorID int64, objectURL string) (string, error) {
	objectURL = CanonicalActorURL(objectURL)
	var id string
	err := pool.QueryRow(ctx, `
		SELECT announce_activity_id FROM federated_announces WHERE actor_id = $1 AND object_url = $2
	`, actorID, objectURL).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ActorHasLikedObject reports whether actorID has a like on objectURL.
func ActorHasLikedObject(ctx context.Context, pool *pgxpool.Pool, actorID int64, objectURL string) (bool, error) {
	objectURL = CanonicalActorURL(objectURL)
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM federated_likes WHERE actor_id = $1 AND object_url = $2
	`, actorID, objectURL).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ActorHasAnnouncedObject reports whether actorID has a boost on objectURL.
func ActorHasAnnouncedObject(ctx context.Context, pool *pgxpool.Pool, actorID int64, objectURL string) (bool, error) {
	objectURL = CanonicalActorURL(objectURL)
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM federated_announces WHERE actor_id = $1 AND object_url = $2
	`, actorID, objectURL).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListActorIDsWhoLikedObject returns actor ids who liked the Note (newest first by federated_likes row id).
func ListActorIDsWhoLikedObject(ctx context.Context, pool *pgxpool.Pool, objectURL string, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 40
	}
	if limit > 80 {
		limit = 80
	}
	objectURL = CanonicalActorURL(objectURL)
	rows, err := pool.Query(ctx, `
		SELECT actor_id FROM federated_likes WHERE object_url = $1 ORDER BY id DESC LIMIT $2
	`, objectURL, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListActorIDsWhoAnnouncedObject returns actor ids who boosted the Note (newest first).
func ListActorIDsWhoAnnouncedObject(ctx context.Context, pool *pgxpool.Pool, objectURL string, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 40
	}
	if limit > 80 {
		limit = 80
	}
	objectURL = CanonicalActorURL(objectURL)
	rows, err := pool.Query(ctx, `
		SELECT actor_id FROM federated_announces WHERE object_url = $1 ORDER BY id DESC LIMIT $2
	`, objectURL, limit)
	if err != nil {
		return nil, fmt.Errorf("announcing actors: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListRecentLikeActivitiesForActor returns Like activities authored by actorID (newest first).
func ListRecentLikeActivitiesForActor(ctx context.Context, pool *pgxpool.Pool, actorID int64, limit int) ([]ActivityRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 80 {
		limit = 80
	}
	rows, err := pool.Query(ctx, `
		SELECT id, activity_id, actor_id, type, raw_json, deleted_at
		FROM activities
		WHERE actor_id = $1 AND lower(type) = 'like'
		ORDER BY id DESC
		LIMIT $2
	`, actorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityRows(rows)
}
