// Package store defines persistence interfaces for ActivityPub aggregates.
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is satisfied by *pgxpool.Pool for health and future repositories.
type Pool interface {
	Ping(context.Context) error
}

// Postgres holds a connection pool for core relational data.
type Postgres struct {
	Pool *pgxpool.Pool
}

// Ping implements Pool.
func (p *Postgres) Ping(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}
