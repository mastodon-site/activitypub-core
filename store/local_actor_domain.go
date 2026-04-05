package store

import (
	"context"
	"strings"

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

// LocalActorIDForInstanceUsernameCI looks up actors.id for this instance's domain using case-insensitive username.
// Used for Mastodon account search when the client sends a bare handle without @domain.
func LocalActorIDForInstanceUsernameCI(ctx context.Context, pool *pgxpool.Pool, instanceHost, username string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		SELECT id FROM actors
		WHERE lower(domain) = lower($1) AND lower(username) = lower($2)
		LIMIT 1
	`, strings.TrimSpace(instanceHost), strings.TrimSpace(username)).Scan(&id)
	return id, err
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
