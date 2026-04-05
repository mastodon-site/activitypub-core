package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func hashSecret(raw string) []byte {
	s := sha256.Sum256([]byte(raw))
	return s[:]
}

// OAuthApplication mirrors DB row for apps API.
type OAuthApplication struct {
	ID           int64
	ClientID     string
	RedirectURIs string
	ClientName   string
	Website      string
	Scopes       string
	ClientSecret string // only set on create response (plaintext once)
	SecretHash   []byte // internal
}

// InsertOAuthApplication generates client_id/secret, stores hashed secret, returns with plaintext secret.
func InsertOAuthApplication(ctx context.Context, pool *pgxpool.Pool, name, redirectURIs, website, scopes string) (*OAuthApplication, error) {
	if strings.TrimSpace(redirectURIs) == "" {
		return nil, fmt.Errorf("redirect_uris required")
	}
	clientID := randomTokenURL(22)
	clientSecret := randomTokenURL(32)
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO oauth_applications (client_id, client_secret_hash, redirect_uris, client_name, website, scopes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, clientID, hashSecret(clientSecret), redirectURIs, name, website, scopes).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &OAuthApplication{
		ID:           id,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		SecretHash:   hashSecret(clientSecret),
		RedirectURIs: redirectURIs,
		ClientName:   name,
		Website:      website,
		Scopes:       scopes,
	}, nil
}

// OAuthApplicationByClientID loads app including secret hash for validation.
func OAuthApplicationByClientID(ctx context.Context, pool *pgxpool.Pool, clientID string) (OAuthApplication, error) {
	var app OAuthApplication
	err := pool.QueryRow(ctx, `
		SELECT id, client_id, client_secret_hash, redirect_uris, client_name, website, scopes
		FROM oauth_applications WHERE client_id = $1
	`, clientID).Scan(&app.ID, &app.ClientID, &app.SecretHash, &app.RedirectURIs, &app.ClientName, &app.Website, &app.Scopes)
	return app, err
}

func VerifyClientSecret(app OAuthApplication, clientSecret string) bool {
	if len(clientSecret) == 0 {
		return false
	}
	g := hashSecret(clientSecret)
	return subtle.ConstantTimeCompare(app.SecretHash, g) == 1
}

// RedirectURIAllowed checks exact match against space or newline separated URIs from apps registration.
func RedirectURIAllowed(registered, candidate string) bool {
	registered = strings.TrimSpace(registered)
	candidate = strings.TrimSpace(candidate)
	if registered == "" || candidate == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(registered, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t'
	}) {
		if strings.TrimSpace(part) == candidate {
			return true
		}
	}
	return false
}

// InsertAuthorizationCode stores a short-lived code for OAuth exchange.
func InsertAuthorizationCode(ctx context.Context, pool *pgxpool.Pool, appID, actorID int64, redirectURI, scopes, challenge, challengeMethod string) (code string, err error) {
	code = randomTokenURL(32)
	expires := time.Now().UTC().Add(10 * time.Minute)
	_, err = pool.Exec(ctx, `
		INSERT INTO oauth_authorization_codes (code, application_id, actor_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
	`, code, appID, actorID, redirectURI, scopes, challenge, challengeMethod, expires)
	if err != nil {
		return "", err
	}
	return code, nil
}

type AuthCodeRow struct {
	ApplicationID int64
	ActorID       int64
	RedirectURI   string
	Scopes        string
	Challenge     string
	ChallengeMeth string
}

// ConsumeAuthorizationCode deletes and returns the row if valid.
func ConsumeAuthorizationCode(ctx context.Context, tx pgx.Tx, code, redirectURI string) (AuthCodeRow, error) {
	var row AuthCodeRow
	err := tx.QueryRow(ctx, `
		DELETE FROM oauth_authorization_codes
		WHERE code = $1 AND redirect_uri = $2 AND expires_at > now()
		RETURNING application_id, actor_id, redirect_uri, scopes, COALESCE(code_challenge, ''), COALESCE(code_challenge_method, '')
	`, code, redirectURI).Scan(&row.ApplicationID, &row.ActorID, &row.RedirectURI, &row.Scopes, &row.Challenge, &row.ChallengeMeth)
	return row, err
}

// InsertAccessToken returns raw token (plaintext once) and stores hash.
func InsertAccessToken(ctx context.Context, pool *pgxpool.Pool, appID, actorID int64, scopes string) (rawToken string, err error) {
	rawToken = randomTokenURL(32)
	th := hashSecret(rawToken)
	_, err = pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, $4)
	`, th, appID, actorID, scopes)
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

// InsertAccessTokenTx is like InsertAccessToken inside an existing transaction.
func InsertAccessTokenTx(ctx context.Context, tx pgx.Tx, appID, actorID int64, scopes string) (rawToken string, err error) {
	rawToken = randomTokenURL(32)
	th := hashSecret(rawToken)
	_, err = tx.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, $4)
	`, th, appID, actorID, scopes)
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

// ActorIDForAccessToken resolves bearer token to local actor id (or 0 for app-only tokens).
func ActorIDForAccessToken(ctx context.Context, pool *pgxpool.Pool, rawToken string) (actorID int64, appID int64, scopes string, err error) {
	th := hashSecret(rawToken)
	var actor sql.NullInt64
	err = pool.QueryRow(ctx, `
		SELECT actor_id, application_id, scopes FROM oauth_access_tokens WHERE token_hash = $1
	`, th).Scan(&actor, &appID, &scopes)
	if err != nil {
		return 0, 0, "", err
	}
	if actor.Valid {
		actorID = actor.Int64
	}
	return actorID, appID, scopes, nil
}

// InsertAppAccessTokenTx creates an application-level token (no user actor), e.g. client_credentials.
func InsertAppAccessTokenTx(ctx context.Context, tx pgx.Tx, appID int64, scopes string) (rawToken string, err error) {
	rawToken = randomTokenURL(32)
	th := hashSecret(rawToken)
	_, err = tx.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, NULL, $3)
	`, th, appID, scopes)
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

func randomTokenURL(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ActorForMastodon returns username, domain, db id, actor URL, and created time for a Mastodon Account entity.
func ActorForMastodon(ctx context.Context, pool *pgxpool.Pool, actorID int64) (username, domain string, dbID int64, actorURL string, createdAt time.Time, err error) {
	err = pool.QueryRow(ctx, `
		SELECT username, domain, id, actor_url, created_at FROM actors WHERE id = $1
	`, actorID).Scan(&username, &domain, &dbID, &actorURL, &createdAt)
	return username, domain, dbID, actorURL, createdAt, err
}

// ListAcceptedFollowerActorURLs returns IRIs of actors who follow followeeID (accepted).
func ListAcceptedFollowerActorURLs(ctx context.Context, pool *pgxpool.Pool, followeeActorID int64) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.actor_url FROM follows f
		JOIN actors a ON a.id = f.follower_actor_id
		WHERE f.followee_actor_id = $1 AND f.state = 'accepted'
	`, followeeActorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	return urls, rows.Err()
}
