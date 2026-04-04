package inboxproc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/as2"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/store"
)

func handleLike(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	objectURL, err := as2.ObjectIRI(fields)
	if err != nil {
		return err
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		return nil
	}
	return store.UpsertFederatedLike(ctx, pool, row.ActorID, objectURL, row.ActivityID)
}

func handleAnnounce(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	objectURL, err := as2.ObjectIRI(fields)
	if err != nil {
		return err
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		return nil
	}
	return store.UpsertFederatedAnnounce(ctx, pool, row.ActorID, objectURL, row.ActivityID)
}

func handleBlock(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	blockedURL, err := as2.ObjectIRI(fields)
	if err != nil {
		return err
	}
	if blockedURL == "" {
		return fmt.Errorf("block: empty object")
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		return nil
	}
	return store.UpsertFederatedBlock(ctx, pool, row.ActorID, blockedURL, row.ActivityID)
}
