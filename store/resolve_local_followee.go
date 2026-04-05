package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// rowQuerier matches *pgxpool.Pool and pgx.Tx for local actor resolution queries.
type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ResolveLocalFolloweeFromObjectIRI returns the local actors row targeted by Follow.object.
// Remote Mastodon stacks often use /@handle, /users/handle, or different casing than the DB username;
// workers may also run with a stale AP_LOCAL_USERNAMES list, so this prefers database matching.
func ResolveLocalFolloweeFromObjectIRI(ctx context.Context, q rowQuerier, cfg *config.Config, objectIRI string) (followeeID int64, localUsername string, err error) {
	objectIRI = strings.TrimSpace(objectIRI)
	if q == nil || cfg == nil || objectIRI == "" {
		return 0, "", fmt.Errorf("follow object is not a local actor")
	}
	base, err := url.Parse(strings.TrimSpace(cfg.PublicBaseURL))
	if err != nil || base.Hostname() == "" {
		return 0, "", fmt.Errorf("follow object is not a local actor")
	}
	instDomain := base.Hostname()

	if cfg.IsLocalActorFollowersOrFollowingCollectionIRI(objectIRI) {
		return 0, "", fmt.Errorf("follow object must be an actor, not followers/following collection")
	}

	canon := CanonicalActorURL(objectIRI)

	var id int64
	var username string
	err = q.QueryRow(ctx, `
		SELECT id, username FROM actors
		WHERE trim(trailing '/' from actor_url) = $1 AND lower(domain) = lower($2)
	`, canon, instDomain).Scan(&id, &username)
	if err == nil {
		return id, username, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", err
	}

	if u, ok := cfg.LocalUsernameForInboundFollowObject(canon); ok {
		fid, err := ActorIDByActorURLQ(ctx, q, cfg.LocalActorProfileURL(u))
		if err != nil {
			return 0, "", fmt.Errorf("followee actor: %w", err)
		}
		return fid, u, nil
	}

	obj, err := url.Parse(objectIRI)
	if err != nil || !strings.EqualFold(obj.Hostname(), instDomain) {
		return 0, "", fmt.Errorf("follow object is not a local actor")
	}
	path := strings.Trim(obj.Path, "/")
	if path == "" {
		return 0, "", fmt.Errorf("follow object is not a local actor")
	}
	if strings.Contains(path, "/followers") || strings.Contains(path, "/following") {
		return 0, "", fmt.Errorf("follow object must be an actor, not followers/following collection")
	}

	handle := ""
	if strings.HasPrefix(path, "@") {
		rest := strings.TrimPrefix(path, "@")
		handle, _, _ = strings.Cut(rest, "/")
	} else if parts := strings.Split(path, "/"); len(parts) >= 2 && parts[0] == "users" {
		handle = parts[1]
	} else {
		return 0, "", fmt.Errorf("follow object is not a local actor")
	}
	handle, err = url.PathUnescape(handle)
	if err != nil || strings.TrimSpace(handle) == "" {
		return 0, "", fmt.Errorf("follow object is not a local actor")
	}

	err = q.QueryRow(ctx, `
		SELECT id, username FROM actors
		WHERE lower(domain) = lower($1) AND lower(username) = lower($2)
		LIMIT 1
	`, instDomain, handle).Scan(&id, &username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", fmt.Errorf("follow object is not a local actor")
		}
		return 0, "", err
	}
	return id, username, nil
}
