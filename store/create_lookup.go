package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FindCreateActivityByObjectNoteIRI returns a recent Create activity whose Note object id matches noteIRI.
func FindCreateActivityByObjectNoteIRI(ctx context.Context, pool *pgxpool.Pool, noteIRI string) (*ActivityRow, error) {
	noteIRI = CanonicalActorURL(noteIRI)
	var r ActivityRow
	err := pool.QueryRow(ctx, `
		SELECT id, activity_id, actor_id, type, raw_json, deleted_at
		FROM activities
		WHERE lower(type) = 'create'
		  AND deleted_at IS NULL
		  AND raw_json::jsonb->'object'->>'id' = $1
		ORDER BY id DESC
		LIMIT 1
	`, noteIRI).Scan(&r.ID, &r.ActivityID, &r.ActorID, &r.Type, &r.RawJSON, &r.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}
	return &r, nil
}
