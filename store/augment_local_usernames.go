package store

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// AugmentLocalUsernamesFromDB appends actors.username rows for this instance's domain into cfg.LocalUsernames
// when missing (deduped with existing entries and with LocalUsername when LocalUsernames is empty).
//
// apd merges this way when constructing the HTTP handler; apw must do the same so process-time helpers
// (e.g. LocalUsernameForInboundFollowObject / IsLocalUsername) recognize accounts created via the API
// or apadmin without an AP_LOCAL_USERNAMES update.
func AugmentLocalUsernamesFromDB(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	if pool == nil || cfg == nil {
		return nil
	}
	pub := strings.TrimSpace(cfg.PublicBaseURL)
	if pub == "" {
		return nil
	}
	base, err := url.Parse(pub)
	if err != nil || base.Hostname() == "" {
		return nil
	}
	fromDB, err := ListLocalActorsOnDomain(ctx, pool, base.Hostname())
	if err != nil {
		return fmt.Errorf("augment local usernames: %w", err)
	}
	mergeCfgLocalUsernames(cfg, fromDB)
	return nil
}

func mergeCfgLocalUsernames(cfg *config.Config, fromDB map[string]int64) {
	// config.Load leaves LocalUsernames empty when only AP_LOCAL_USERNAME is set; IsLocalUsername
	// falls back to LocalUsername only in that empty case. Once we append DB usernames, the slice
	// is non-empty and we must include LocalUsername explicitly so it stays recognized.
	if len(cfg.LocalUsernames) == 0 && strings.TrimSpace(cfg.LocalUsername) != "" {
		cfg.LocalUsernames = []string{cfg.LocalUsername}
	}
	seen := make(map[string]struct{})
	for _, u := range cfg.LocalUsernames {
		seen[u] = struct{}{}
	}
	for u := range fromDB {
		if _, ok := seen[u]; !ok {
			cfg.LocalUsernames = append(cfg.LocalUsernames, u)
			seen[u] = struct{}{}
		}
	}
}
