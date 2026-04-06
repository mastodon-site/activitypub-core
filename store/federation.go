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

// LocalActorPublicKeyPlaceholder is stored when no real key PEM is configured yet.
const LocalActorPublicKeyPlaceholder = "(local-public-key-unconfigured)"

type dbQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// EnsureLocalActor upserts a local Person (username on this instance) and returns its database id.
func EnsureLocalActor(ctx context.Context, pool dbQueryRow, cfg *config.Config, username string, publicKeyPEM string) (int64, error) {
	if !cfg.IsLocalUsername(username) {
		return 0, fmt.Errorf("username %q is not a configured local account", username)
	}
	return UpsertLocalActor(ctx, pool, cfg, username, publicKeyPEM)
}

// UpsertLocalActor upserts a local Person without checking AP_LOCAL_USERNAMES (e.g. apadmin-created accounts).
func UpsertLocalActor(ctx context.Context, pool dbQueryRow, cfg *config.Config, username string, publicKeyPEM string) (int64, error) {
	if cfg.PublicBaseURL == "" {
		return 0, fmt.Errorf("AP_PUBLIC_BASE_URL required for local actor")
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
	actorURL := root + "/@" + url.PathEscape(username)
	inboxURL := cfg.LocalActorInboxURL(username)
	outboxURL := root + "/@" + url.PathEscape(username) + "/outbox"
	pem := publicKeyPEM
	if strings.TrimSpace(pem) == "" {
		pem = LocalActorPublicKeyPlaceholder
	}
	var id int64
	err = pool.QueryRow(ctx, `
		INSERT INTO actors (username, domain, actor_url, inbox_url, outbox_url, public_key_pem)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (username, domain) DO UPDATE SET
			actor_url = EXCLUDED.actor_url,
			inbox_url = EXCLUDED.inbox_url,
			outbox_url = EXCLUDED.outbox_url,
			public_key_pem = CASE
				WHEN coalesce(trim(actors.private_key_pem), '') <> '' THEN actors.public_key_pem
				ELSE EXCLUDED.public_key_pem
			END,
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
// If maxID is non-nil, only rows with activities.id strictly less than *maxID are returned (older page).
// If sinceID is non-nil, only rows with activities.id strictly greater than *sinceID are returned.
// nextCursor is the smallest activities.id among returned rows (for the next page's max_id); nil if no next page.
func OutboxPage(ctx context.Context, pool *pgxpool.Pool, localActorDBID int64, limit int, maxID, sinceID *int64) (total int64, items []string, nextCursor *int64, err error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM activities WHERE actor_id = $1`, localActorDBID).Scan(&total); err != nil {
		return 0, nil, nil, fmt.Errorf("outbox count: %w", err)
	}
	var rows pgx.Rows
	switch {
	case maxID != nil && sinceID != nil:
		rows, err = pool.Query(ctx, `
			SELECT id, activity_id FROM activities
			WHERE actor_id = $1 AND id < $2 AND id > $3
			ORDER BY id DESC LIMIT $4
		`, localActorDBID, *maxID, *sinceID, limit)
	case maxID != nil:
		rows, err = pool.Query(ctx, `
			SELECT id, activity_id FROM activities
			WHERE actor_id = $1 AND id < $2
			ORDER BY id DESC LIMIT $3
		`, localActorDBID, *maxID, limit)
	case sinceID != nil:
		rows, err = pool.Query(ctx, `
			SELECT id, activity_id FROM activities
			WHERE actor_id = $1 AND id > $2
			ORDER BY id DESC LIMIT $3
		`, localActorDBID, *sinceID, limit)
	default:
		rows, err = pool.Query(ctx, `
			SELECT id, activity_id FROM activities
			WHERE actor_id = $1
			ORDER BY id DESC LIMIT $2
		`, localActorDBID, limit)
	}
	if err != nil {
		return 0, nil, nil, fmt.Errorf("outbox list: %w", err)
	}
	defer rows.Close()
	var ids []int64
	items = make([]string, 0)
	for rows.Next() {
		var dbID int64
		var iri string
		if err := rows.Scan(&dbID, &iri); err != nil {
			return 0, nil, nil, err
		}
		ids = append(ids, dbID)
		items = append(items, iri)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, nil, err
	}
	if len(items) == int(limit) && len(ids) > 0 {
		last := ids[len(ids)-1]
		nextCursor = &last
	}
	return total, items, nextCursor, nil
}
