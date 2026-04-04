package inboxproc

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func actorURLByDBID(ctx context.Context, pool *pgxpool.Pool, actorDBID int64) (string, error) {
	var u string
	err := pool.QueryRow(ctx, `SELECT actor_url FROM actors WHERE id = $1`, actorDBID).Scan(&u)
	if err != nil {
		return "", fmt.Errorf("actor %d: %w", actorDBID, err)
	}
	return u, nil
}
