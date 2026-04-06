package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListRecentDirectCreatesInvolvingActor returns recent direct visibility Creates where the viewer is author or listed as recipient.
func ListRecentDirectCreatesInvolvingActor(ctx context.Context, pool *pgxpool.Pool, viewerActorID int64, instanceDomain string, limit int) ([]ActivityRow, error) {
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
		SELECT a.id, a.activity_id, a.actor_id, a.type, a.raw_json, a.deleted_at
		FROM activities a
		INNER JOIN actors act ON act.id = a.actor_id
		WHERE lower(a.type) = 'create'
		  AND a.deleted_at IS NULL
		  AND lower(act.domain) = lower($2)
		  AND raw_json::jsonb->'object'->>'_visibility' = 'direct'
		  AND (
			a.actor_id = $1
			OR (raw_json::jsonb->'object'->'_directRecipientActorIds' @> jsonb_build_array($1::bigint))
		  )
		ORDER BY a.id DESC
		LIMIT $3
	`, viewerActorID, instanceDomain, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityRows(rows)
}
