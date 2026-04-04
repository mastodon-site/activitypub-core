package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

const localActorKeyPlaceholder = "(local-public-key-unconfigured)"

type dbQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// EnsureLocalActor upserts a local Person (username on this instance) and returns its database id.
func EnsureLocalActor(ctx context.Context, pool dbQueryRow, cfg *config.Config, username string, publicKeyPEM string) (int64, error) {
	if cfg.PublicBaseURL == "" {
		return 0, fmt.Errorf("AP_PUBLIC_BASE_URL required for local actor")
	}
	if !cfg.IsLocalUsername(username) {
		return 0, fmt.Errorf("username %q is not a configured local account", username)
	}
	base, err := url.Parse(cfg.PublicBaseURL)
	if err != nil {
		return 0, fmt.Errorf("AP_PUBLIC_BASE_URL: %w", err)
	}
	domain := base.Hostname()
	if domain == "" {
		return 0, fmt.Errorf("AP_PUBLIC_BASE_URL missing host")
	}
	root := strings.TrimRight(cfg.PublicBaseURL, "/")
	actorURL := root + "/users/" + url.PathEscape(username)
	inboxURL := root + "/inbox"
	outboxURL := root + "/outbox/" + url.PathEscape(username)
	pem := publicKeyPEM
	if strings.TrimSpace(pem) == "" {
		pem = localActorKeyPlaceholder
	}
	var id int64
	err = pool.QueryRow(ctx, `
		INSERT INTO actors (username, domain, actor_url, inbox_url, outbox_url, public_key_pem)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (actor_url) DO UPDATE SET
			public_key_pem = EXCLUDED.public_key_pem,
			inbox_url = EXCLUDED.inbox_url,
			outbox_url = EXCLUDED.outbox_url,
			updated_at = now()
		RETURNING id
	`, username, domain, actorURL, inboxURL, outboxURL, pem).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure local actor: %w", err)
	}
	return id, nil
}

// EnsureRemoteActor upserts a federated actor row (for activities.actor_id FK).
func EnsureRemoteActor(ctx context.Context, pool dbQueryRow, actorURL string, publicKeyPEM string) (int64, error) {
	u, err := url.Parse(actorURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return 0, fmt.Errorf("invalid actor URL %q", actorURL)
	}
	domain := u.Hostname()
	path := strings.Trim(u.Path, "/")
	var username string
	if path != "" {
		parts := strings.Split(path, "/")
		username, _ = url.PathUnescape(parts[len(parts)-1])
	}
	if username == "" {
		return 0, fmt.Errorf("could not derive username from actor URL %q", actorURL)
	}
	canon := strings.TrimRight(actorURL, "/")
	var id int64
	err = pool.QueryRow(ctx, `
		INSERT INTO actors (username, domain, actor_url, inbox_url, outbox_url, public_key_pem)
		VALUES ($1, $2, $3, '', '', $4)
		ON CONFLICT (actor_url) DO UPDATE SET
			public_key_pem = EXCLUDED.public_key_pem,
			updated_at = now()
		RETURNING id
	`, username, domain, canon, publicKeyPEM).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure remote actor: %w", err)
	}
	return id, nil
}

// InsertInboundActivity persists a verified inbound activity. inserted is false if activity_id was already stored.
func InsertInboundActivity(ctx context.Context, q dbQueryRow, remoteActorDBID int64, activityID, activityType string, rawJSON []byte) (inserted bool, newID int64, err error) {
	activityType = strings.TrimSpace(activityType)
	if activityType == "" {
		activityType = "unknown"
	}
	row := q.QueryRow(ctx, `
		INSERT INTO activities (activity_id, actor_id, type, raw_json)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (activity_id) DO NOTHING
		RETURNING id
	`, activityID, remoteActorDBID, activityType, rawJSON)
	err = row.Scan(&newID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	return true, newID, nil
}

// OutboxPage returns total outbound activities for the local actor and up to limit IRIs (newest first).
func OutboxPage(ctx context.Context, pool *pgxpool.Pool, localActorDBID int64, limit int) (total int64, items []string, err error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM activities WHERE actor_id = $1`, localActorDBID).Scan(&total); err != nil {
		return 0, nil, fmt.Errorf("outbox count: %w", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT activity_id FROM activities WHERE actor_id = $1 ORDER BY id DESC LIMIT $2
	`, localActorDBID, limit)
	if err != nil {
		return 0, nil, fmt.Errorf("outbox list: %w", err)
	}
	defer rows.Close()
	items = make([]string, 0)
	for rows.Next() {
		var iri string
		if err := rows.Scan(&iri); err != nil {
			return 0, nil, err
		}
		items = append(items, iri)
	}
	return total, items, rows.Err()
}
