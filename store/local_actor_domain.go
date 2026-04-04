package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListLocalActorsOnDomain returns username -> actors.id for rows whose domain matches this instance host.
func ListLocalActorsOnDomain(ctx context.Context, pool *pgxpool.Pool, domain string) (map[string]int64, error) {
	rows, err := pool.Query(ctx, `SELECT username, id FROM actors WHERE domain = $1`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var u string
		var id int64
		if err := rows.Scan(&u, &id); err != nil {
			return nil, err
		}
		out[u] = id
	}
	return out, rows.Err()
}

// LocalActorUsernameExists reports whether an actor row exists for this instance domain and username.
func LocalActorUsernameExists(ctx context.Context, pool *pgxpool.Pool, domain, username string) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM actors WHERE domain = $1 AND username = $2
	`, domain, username).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
