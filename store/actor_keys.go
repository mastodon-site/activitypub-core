package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ActorPublicKeyPEMForLocalUser returns stored public_key_pem for a local actor on instanceDomain.
func ActorPublicKeyPEMForLocalUser(ctx context.Context, pool *pgxpool.Pool, instanceDomain, username string) (string, error) {
	instanceDomain = strings.TrimSpace(instanceDomain)
	username = strings.TrimSpace(username)
	if instanceDomain == "" || username == "" {
		return "", fmt.Errorf("domain and username required")
	}
	var pem string
	err := pool.QueryRow(ctx, `
		SELECT public_key_pem FROM actors
		WHERE lower(domain) = lower($1) AND lower(username) = lower($2)
	`, instanceDomain, username).Scan(&pem)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(pem), nil
}

// ActorHasStoredPrivateKey reports whether the actor row has a non-empty private_key_pem.
func ActorHasStoredPrivateKey(ctx context.Context, pool *pgxpool.Pool, instanceDomain, username string) (bool, error) {
	instanceDomain = strings.TrimSpace(instanceDomain)
	username = strings.TrimSpace(username)
	if instanceDomain == "" || username == "" {
		return false, fmt.Errorf("domain and username required")
	}
	var ok bool
	err := pool.QueryRow(ctx, `
		SELECT coalesce(trim(private_key_pem), '') <> ''
		FROM actors
		WHERE lower(domain) = lower($1) AND lower(username) = lower($2)
	`, instanceDomain, username).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ActorPrivateKeyPEMForLocalUser returns PKCS#8 PEM private key for delivery signing, if stored.
func ActorPrivateKeyPEMForLocalUser(ctx context.Context, pool *pgxpool.Pool, instanceDomain, username string) (string, error) {
	instanceDomain = strings.TrimSpace(instanceDomain)
	username = strings.TrimSpace(username)
	if instanceDomain == "" || username == "" {
		return "", fmt.Errorf("domain and username required")
	}
	var pemStr string
	err := pool.QueryRow(ctx, `
		SELECT private_key_pem FROM actors
		WHERE lower(domain) = lower($1) AND lower(username) = lower($2)
	`, instanceDomain, username).Scan(&pemStr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(pemStr), nil
}

// SetActorRSAKeypair stores per-actor PKCS#8 private PEM and PKIX public PEM (used after UpsertLocalActor).
func SetActorRSAKeypair(ctx context.Context, pool *pgxpool.Pool, actorID int64, privateKeyPEM, publicKeyPEM string) error {
	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	publicKeyPEM = strings.TrimSpace(publicKeyPEM)
	if privateKeyPEM == "" || publicKeyPEM == "" {
		return fmt.Errorf("private and public PEM required")
	}
	_, err := pool.Exec(ctx, `
		UPDATE actors SET private_key_pem = $2, public_key_pem = $3, updated_at = now()
		WHERE id = $1
	`, actorID, privateKeyPEM, publicKeyPEM)
	return err
}
