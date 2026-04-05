package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertStatusBookmark records a bookmark for a Create-activity status row (activities.id).
func UpsertStatusBookmark(ctx context.Context, pool *pgxpool.Pool, actorID, statusActivityID int64) error {
	if actorID < 1 || statusActivityID < 1 {
		return fmt.Errorf("bookmark: invalid id")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO status_bookmarks (actor_id, status_activity_id)
		VALUES ($1, $2)
		ON CONFLICT (actor_id, status_activity_id) DO UPDATE SET created_at = now()
	`, actorID, statusActivityID)
	return err
}

// DeleteStatusBookmark removes a bookmark. Returns deleted count (0 or 1).
func DeleteStatusBookmark(ctx context.Context, pool *pgxpool.Pool, actorID, statusActivityID int64) (int64, error) {
	tag, err := pool.Exec(ctx, `
		DELETE FROM status_bookmarks WHERE actor_id = $1 AND status_activity_id = $2
	`, actorID, statusActivityID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// StatusBookmarked reports whether actor bookmarked this status activity id.
func StatusBookmarked(ctx context.Context, pool *pgxpool.Pool, actorID, statusActivityID int64) (bool, error) {
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM status_bookmarks WHERE actor_id = $1 AND status_activity_id = $2
	`, actorID, statusActivityID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListBookmarkedStatusActivityIDs returns newest bookmarked status activity ids for an actor.
func ListBookmarkedStatusActivityIDs(ctx context.Context, pool *pgxpool.Pool, actorID int64, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 80 {
		limit = 80
	}
	rows, err := pool.Query(ctx, `
		SELECT status_activity_id FROM status_bookmarks
		WHERE actor_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, actorID, limit)
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
