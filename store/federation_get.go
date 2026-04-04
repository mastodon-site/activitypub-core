package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GetActivityJSONByIRIs returns raw Activity JSON and author actor_url when activity_id matches
// one of candidateIRIs. Not found is (nil, "", nil).
func GetActivityJSONByIRIs(ctx context.Context, pool *pgxpool.Pool, candidateIRIs []string) (rawJSON []byte, actorURL string, err error) {
	if len(candidateIRIs) == 0 {
		return nil, "", nil
	}
	row := pool.QueryRow(ctx, `
		SELECT a.raw_json, act.actor_url
		FROM activities a
		JOIN actors act ON act.id = a.actor_id
		WHERE a.activity_id = ANY($1::text[])
		LIMIT 1
	`, candidateIRIs)
	var raw []byte
	if err := row.Scan(&raw, &actorURL); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("activity lookup: %w", err)
	}
	return raw, actorURL, nil
}

// GetObjectJSONByIRIs returns raw Object JSON and owner actor_url for a non-deleted object
// when object_url matches one of candidateIRIs.
func GetObjectJSONByIRIs(ctx context.Context, pool *pgxpool.Pool, candidateIRIs []string) (rawJSON []byte, actorURL string, err error) {
	if len(candidateIRIs) == 0 {
		return nil, "", nil
	}
	row := pool.QueryRow(ctx, `
		SELECT o.raw_json, act.actor_url
		FROM objects o
		JOIN actors act ON act.id = o.actor_id
		WHERE o.object_url = ANY($1::text[]) AND o.deleted_at IS NULL
		LIMIT 1
	`, candidateIRIs)
	var raw []byte
	if err := row.Scan(&raw, &actorURL); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("object lookup: %w", err)
	}
	return raw, actorURL, nil
}
