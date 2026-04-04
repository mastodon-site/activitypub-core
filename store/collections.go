package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FollowersPage lists accepted followers (actor IRIs) for followeeActorID, newest relationship rows first.
// maxID filters to follows.id < *maxID when set. sinceID filters to follows.id > *sinceID when set.
func FollowersPage(ctx context.Context, pool *pgxpool.Pool, followeeActorID int64, limit int, maxID, sinceID *int64) (total int64, items []string, nextCursor *int64, err error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM follows WHERE followee_actor_id = $1 AND state = 'accepted'
	`, followeeActorID).Scan(&total); err != nil {
		return 0, nil, nil, fmt.Errorf("followers count: %w", err)
	}

	var rows pgx.Rows
	switch {
	case maxID != nil && sinceID != nil:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.follower_actor_id
			WHERE f.followee_actor_id = $1 AND f.state = 'accepted' AND f.id < $2 AND f.id > $3
			ORDER BY f.id DESC LIMIT $4
		`, followeeActorID, *maxID, *sinceID, limit)
	case maxID != nil:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.follower_actor_id
			WHERE f.followee_actor_id = $1 AND f.state = 'accepted' AND f.id < $2
			ORDER BY f.id DESC LIMIT $3
		`, followeeActorID, *maxID, limit)
	case sinceID != nil:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.follower_actor_id
			WHERE f.followee_actor_id = $1 AND f.state = 'accepted' AND f.id > $2
			ORDER BY f.id DESC LIMIT $3
		`, followeeActorID, *sinceID, limit)
	default:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.follower_actor_id
			WHERE f.followee_actor_id = $1 AND f.state = 'accepted'
			ORDER BY f.id DESC LIMIT $2
		`, followeeActorID, limit)
	}
	if err != nil {
		return 0, nil, nil, fmt.Errorf("followers list: %w", err)
	}
	defer rows.Close()
	var ids []int64
	items = make([]string, 0)
	for rows.Next() {
		var fid int64
		var iri string
		if err := rows.Scan(&fid, &iri); err != nil {
			return 0, nil, nil, err
		}
		ids = append(ids, fid)
		items = append(items, iri)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, nil, err
	}
	if len(items) == limit && len(ids) > 0 {
		last := ids[len(ids)-1]
		nextCursor = &last
	}
	return total, items, nextCursor, nil
}

// FollowingPage lists accepted followees (actor IRIs) for followerActorID, newest relationship rows first.
func FollowingPage(ctx context.Context, pool *pgxpool.Pool, followerActorID int64, limit int, maxID, sinceID *int64) (total int64, items []string, nextCursor *int64, err error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM follows WHERE follower_actor_id = $1 AND state = 'accepted'
	`, followerActorID).Scan(&total); err != nil {
		return 0, nil, nil, fmt.Errorf("following count: %w", err)
	}

	var rows pgx.Rows
	switch {
	case maxID != nil && sinceID != nil:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.followee_actor_id
			WHERE f.follower_actor_id = $1 AND f.state = 'accepted' AND f.id < $2 AND f.id > $3
			ORDER BY f.id DESC LIMIT $4
		`, followerActorID, *maxID, *sinceID, limit)
	case maxID != nil:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.followee_actor_id
			WHERE f.follower_actor_id = $1 AND f.state = 'accepted' AND f.id < $2
			ORDER BY f.id DESC LIMIT $3
		`, followerActorID, *maxID, limit)
	case sinceID != nil:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.followee_actor_id
			WHERE f.follower_actor_id = $1 AND f.state = 'accepted' AND f.id > $2
			ORDER BY f.id DESC LIMIT $3
		`, followerActorID, *sinceID, limit)
	default:
		rows, err = pool.Query(ctx, `
			SELECT f.id, a.actor_url
			FROM follows f JOIN actors a ON a.id = f.followee_actor_id
			WHERE f.follower_actor_id = $1 AND f.state = 'accepted'
			ORDER BY f.id DESC LIMIT $2
		`, followerActorID, limit)
	}
	if err != nil {
		return 0, nil, nil, fmt.Errorf("following list: %w", err)
	}
	defer rows.Close()
	var ids []int64
	items = make([]string, 0)
	for rows.Next() {
		var fid int64
		var iri string
		if err := rows.Scan(&fid, &iri); err != nil {
			return 0, nil, nil, err
		}
		ids = append(ids, fid)
		items = append(items, iri)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, nil, err
	}
	if len(items) == limit && len(ids) > 0 {
		last := ids[len(ids)-1]
		nextCursor = &last
	}
	return total, items, nextCursor, nil
}
