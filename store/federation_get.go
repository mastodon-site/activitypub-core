package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GetActivityJSONByIRIs returns raw Activity JSON and author actor_url when activity_id matches
// one of candidateIRIs and the row is not soft-deleted. Not found is (nil, "", nil).
func GetActivityJSONByIRIs(ctx context.Context, pool *pgxpool.Pool, candidateIRIs []string) (rawJSON []byte, actorURL string, err error) {
	if len(candidateIRIs) == 0 {
		return nil, "", nil
	}
	row := pool.QueryRow(ctx, `
		SELECT a.raw_json, act.actor_url
		FROM activities a
		JOIN actors act ON act.id = a.actor_id
		WHERE a.activity_id = ANY($1::text[])
		  AND a.deleted_at IS NULL
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

// DeletedCreateNoteForFederationGET returns the embedded Note object id and author actor_url when a
// soft-deleted local Create activity matches candidate IRIs (by activity_id or by object.id).
// Not found is ("", "", nil).
func DeletedCreateNoteForFederationGET(ctx context.Context, pool *pgxpool.Pool, candidateIRIs []string) (noteIRI string, actorURL string, err error) {
	if len(candidateIRIs) == 0 {
		return "", "", nil
	}
	row := pool.QueryRow(ctx, `
		SELECT COALESCE(a.raw_json::jsonb->'object'->>'id', ''), act.actor_url
		FROM activities a
		JOIN actors act ON act.id = a.actor_id
		WHERE lower(a.type) = 'create'
		  AND a.deleted_at IS NOT NULL
		  AND (
		    a.activity_id = ANY($1::text[])
		    OR a.raw_json::jsonb->'object'->>'id' = ANY($1::text[])
		  )
		LIMIT 1
	`, candidateIRIs)
	if err := row.Scan(&noteIRI, &actorURL); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("deleted create lookup: %w", err)
	}
	if noteIRI == "" {
		return "", "", nil
	}
	return noteIRI, actorURL, nil
}

// DeletedObjectForFederationGET returns object_url and owner actor_url when a soft-deleted object row
// matches candidate IRIs. Not found is ("", "", nil).
func DeletedObjectForFederationGET(ctx context.Context, pool *pgxpool.Pool, candidateIRIs []string) (objectURL string, actorURL string, err error) {
	if len(candidateIRIs) == 0 {
		return "", "", nil
	}
	row := pool.QueryRow(ctx, `
		SELECT o.object_url, act.actor_url
		FROM objects o
		JOIN actors act ON act.id = o.actor_id
		WHERE o.object_url = ANY($1::text[])
		  AND o.deleted_at IS NOT NULL
		LIMIT 1
	`, candidateIRIs)
	if err := row.Scan(&objectURL, &actorURL); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("deleted object lookup: %w", err)
	}
	return objectURL, actorURL, nil
}
