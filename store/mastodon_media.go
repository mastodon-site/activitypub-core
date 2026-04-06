package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertMastodonMedia records an uploaded blob for a local actor.
func InsertMastodonMedia(ctx context.Context, pool *pgxpool.Pool, actorID int64, blobKey, contentType string, byteSize int64) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO mastodon_media (actor_id, blob_key, content_type, byte_size)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, actorID, blobKey, contentType, byteSize).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// MastodonMediaRow is a row from mastodon_media.
type MastodonMediaRow struct {
	ID          int64
	ActorID     int64
	BlobKey     string
	ContentType string
	ByteSize    int64
}

// GetMastodonMediaByID returns media metadata (any owner).
func GetMastodonMediaByID(ctx context.Context, pool *pgxpool.Pool, mediaID int64) (*MastodonMediaRow, error) {
	var r MastodonMediaRow
	err := pool.QueryRow(ctx, `
		SELECT id, actor_id, blob_key, content_type, byte_size
		FROM mastodon_media WHERE id = $1
	`, mediaID).Scan(&r.ID, &r.ActorID, &r.BlobKey, &r.ContentType, &r.ByteSize)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetMastodonMediaForActor returns media metadata if it exists and belongs to actorID.
func GetMastodonMediaForActor(ctx context.Context, pool *pgxpool.Pool, mediaID, actorID int64) (*MastodonMediaRow, error) {
	var r MastodonMediaRow
	err := pool.QueryRow(ctx, `
		SELECT id, actor_id, blob_key, content_type, byte_size
		FROM mastodon_media WHERE id = $1 AND actor_id = $2
	`, mediaID, actorID).Scan(&r.ID, &r.ActorID, &r.BlobKey, &r.ContentType, &r.ByteSize)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateMastodonMediaDescription sets optional alt-text (Mastodon "description") before attach.
// LookupMastodonMediaIDByBlobKey returns mastodon_media.id for an actor-owned blob key.
func LookupMastodonMediaIDByBlobKey(ctx context.Context, pool *pgxpool.Pool, actorID int64, blobKey string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		SELECT id FROM mastodon_media WHERE actor_id = $1 AND blob_key = $2
	`, actorID, blobKey).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
		return 0, err
	}
	return id, nil
}

func UpdateMastodonMediaDescription(ctx context.Context, pool *pgxpool.Pool, mediaID, actorID int64, description string) error {
	tag, err := pool.Exec(ctx, `
		UPDATE mastodon_media SET description = $3
		WHERE id = $1 AND actor_id = $2
	`, mediaID, actorID, description)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}
