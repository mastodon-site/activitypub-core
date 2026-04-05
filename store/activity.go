package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActivityRow is a persisted activity used by async inbox processors.
type ActivityRow struct {
	ID         int64
	ActivityID string
	ActorID    int64
	Type       string
	RawJSON    []byte
}

// GetActivityByID loads an activity by primary key.
func GetActivityByID(ctx context.Context, pool *pgxpool.Pool, id int64) (*ActivityRow, error) {
	var r ActivityRow
	err := pool.QueryRow(ctx, `
		SELECT id, activity_id, actor_id, type, raw_json FROM activities WHERE id = $1
	`, id).Scan(&r.ID, &r.ActivityID, &r.ActorID, &r.Type, &r.RawJSON)
	if err != nil {
		return nil, fmt.Errorf("activity %d: %w", id, err)
	}
	return &r, nil
}

// ListRecentCreateActivitiesForActor returns recent Create activities for one actor (newest first).
func ListRecentCreateActivitiesForActor(ctx context.Context, pool *pgxpool.Pool, actorID int64, limit int) ([]ActivityRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 80 {
		limit = 80
	}
	rows, err := pool.Query(ctx, `
		SELECT id, activity_id, actor_id, type, raw_json
		FROM activities
		WHERE actor_id = $1 AND lower(type) = 'create'
		ORDER BY id DESC
		LIMIT $2
	`, actorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list creates: %w", err)
	}
	defer rows.Close()
	return scanActivityRows(rows)
}

// ListRecentPublicCreateActivities returns recent Create activities from actors on instanceDomain (newest first).
func ListRecentPublicCreateActivities(ctx context.Context, pool *pgxpool.Pool, instanceDomain string, limit int) ([]ActivityRow, error) {
	instanceDomain = strings.TrimSpace(instanceDomain)
	if instanceDomain == "" {
		return nil, fmt.Errorf("instance domain required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 80 {
		limit = 80
	}
	rows, err := pool.Query(ctx, `
		SELECT a.id, a.activity_id, a.actor_id, a.type, a.raw_json
		FROM activities a
		INNER JOIN actors act ON act.id = a.actor_id
		WHERE lower(a.type) = 'create' AND lower(act.domain) = lower($1)
		ORDER BY a.id DESC
		LIMIT $2
	`, instanceDomain, limit)
	if err != nil {
		return nil, fmt.Errorf("list public creates: %w", err)
	}
	defer rows.Close()
	return scanActivityRows(rows)
}

func scanActivityRows(rows pgx.Rows) ([]ActivityRow, error) {
	var out []ActivityRow
	for rows.Next() {
		var r ActivityRow
		if err := rows.Scan(&r.ID, &r.ActivityID, &r.ActorID, &r.Type, &r.RawJSON); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ActivityIDByActorAndActivityIRI returns the activities.id for a stored activity_id string.
func ActivityIDByActorAndActivityIRI(ctx context.Context, pool *pgxpool.Pool, actorID int64, activityIRI string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		SELECT id FROM activities WHERE actor_id = $1 AND activity_id = $2
	`, actorID, activityIRI).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// CanonicalActorURL trims space and trailing slashes for comparison.
func CanonicalActorURL(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

// ActorIDByActorURL looks up an actor row by actor IRI.
func ActorIDByActorURL(ctx context.Context, pool *pgxpool.Pool, actorURL string) (int64, error) {
	return ActorIDByActorURLQ(ctx, pool, actorURL)
}

// ActorIDByActorURLQ looks up an actor using any pgx Querier (pool or transaction).
func ActorIDByActorURLQ(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actorURL string) (int64, error) {
	canon := CanonicalActorURL(actorURL)
	var id int64
	err := q.QueryRow(ctx, `
		SELECT id FROM actors WHERE trim(trailing '/' from actor_url) = $1
	`, canon).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ActorProfileByID loads actor_url, username, and domain for a row id.
func ActorProfileByID(ctx context.Context, pool *pgxpool.Pool, id int64) (actorURL, username, domain string, err error) {
	err = pool.QueryRow(ctx, `
		SELECT actor_url, username, domain FROM actors WHERE id = $1
	`, id).Scan(&actorURL, &username, &domain)
	return actorURL, username, domain, err
}
