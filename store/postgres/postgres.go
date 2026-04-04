package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/store"
)

// NewPool opens a pgx pool from databaseURL.
func NewPool(ctx context.Context, databaseURL string) (*store.Postgres, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pgx config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgx pool: %w", err)
	}
	return &store.Postgres{Pool: pool}, nil
}
