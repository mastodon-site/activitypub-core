package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// UpsertLocalAccountPassword sets or replaces bcrypt password for a local actor.
func UpsertLocalAccountPassword(ctx context.Context, pool *pgxpool.Pool, actorID int64, plainPassword string) error {
	if plainPassword == "" {
		return fmt.Errorf("password required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO local_accounts (actor_id, password_bcrypt)
		VALUES ($1, $2)
		ON CONFLICT (actor_id) DO UPDATE SET password_bcrypt = EXCLUDED.password_bcrypt
	`, actorID, string(hash))
	return err
}

// AuthenticateLocalAccount returns actor id if username+password match a local account on this domain.
func AuthenticateLocalAccount(ctx context.Context, pool *pgxpool.Pool, domain, username, plainPassword string) (actorID int64, err error) {
	var hash string
	err = pool.QueryRow(ctx, `
		SELECT a.id, la.password_bcrypt
		FROM actors a
		JOIN local_accounts la ON la.actor_id = a.id
		WHERE a.username = $1 AND a.domain = $2
	`, username, domain).Scan(&actorID, &hash)
	if err != nil {
		return 0, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plainPassword)) != nil {
		return 0, fmt.Errorf("invalid credentials")
	}
	return actorID, nil
}

// LocalAccountExists reports whether the actor has a password row.
func LocalAccountExists(ctx context.Context, pool *pgxpool.Pool, actorID int64) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM local_accounts WHERE actor_id = $1`, actorID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
