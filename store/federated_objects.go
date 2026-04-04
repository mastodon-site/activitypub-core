package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dbExecObj interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// UpsertFederatedObject stores or replaces a copy of an ActivityStreams object.
func UpsertFederatedObject(ctx context.Context, q dbExecObj, objectURL string, ownerActorID int64, typ string, rawJSON []byte) error {
	if objectURL == "" {
		return fmt.Errorf("object url required")
	}
	if typ == "" {
		typ = "Object"
	}
	_, err := q.Exec(ctx, `
		INSERT INTO objects (object_url, actor_id, type, raw_json, deleted_at)
		VALUES ($1, $2, $3, $4, NULL)
		ON CONFLICT (object_url) DO UPDATE SET
			actor_id = EXCLUDED.actor_id,
			type = EXCLUDED.type,
			raw_json = EXCLUDED.raw_json,
			deleted_at = NULL
	`, objectURL, ownerActorID, typ, rawJSON)
	return err
}

// SoftDeleteFederatedObjectByURL sets deleted_at when the row exists.
func SoftDeleteFederatedObjectByURL(ctx context.Context, q dbExecObj, objectURL string) error {
	if objectURL == "" {
		return fmt.Errorf("object url required")
	}
	_, err := q.Exec(ctx, `
		UPDATE objects SET deleted_at = now() WHERE object_url = $1 AND deleted_at IS NULL
	`, objectURL)
	return err
}

// ObjectOwnerActorID returns actor_id for a stored object, if present and not deleted.
func ObjectOwnerActorID(ctx context.Context, pool *pgxpool.Pool, objectURL string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		SELECT actor_id FROM objects WHERE object_url = $1 AND deleted_at IS NULL
	`, objectURL).Scan(&id)
	return id, err
}
