package inboxproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/as2"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/store"
)

func handleCreate(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	rawObj, ok := fields["object"]
	if !ok {
		return fmt.Errorf("create missing object")
	}
	id, typ, rawJSON, err := as2.ObjectFieldIDType(rawObj)
	if err != nil {
		return err
	}
	if len(rawJSON) == 0 {
		// Object given by IRI only; would require a fetch to materialize.
		return nil
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		return nil
	}
	actorIRI, err := as2.ActorIRIFromActivity(fields)
	if err != nil {
		return err
	}
	signerURL, err := actorURLByDBID(ctx, pool, row.ActorID)
	if err != nil {
		return err
	}
	if !actorsMatch(actorIRI, signerURL) {
		return fmt.Errorf("create: activity actor does not match stored actor")
	}
	if !as2.ObjectAttributedToMatches(rawJSON, actorIRI) {
		return fmt.Errorf("create: attributedTo does not match actor")
	}
	return store.UpsertFederatedObject(ctx, pool, id, row.ActorID, typ, rawJSON)
}

func handleUpdate(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	rawObj, ok := fields["object"]
	if !ok {
		return fmt.Errorf("update missing object")
	}
	id, typ, rawJSON, err := as2.ObjectFieldIDType(rawObj)
	if err != nil {
		return err
	}
	if len(rawJSON) == 0 {
		return nil
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		return nil
	}
	actorIRI, err := as2.ActorIRIFromActivity(fields)
	if err != nil {
		return err
	}
	signerURL, err := actorURLByDBID(ctx, pool, row.ActorID)
	if err != nil {
		return err
	}
	if !actorsMatch(actorIRI, signerURL) {
		return fmt.Errorf("update: activity actor does not match stored actor")
	}
	if !as2.ObjectAttributedToMatches(rawJSON, actorIRI) {
		return fmt.Errorf("update: attributedTo does not match actor")
	}
	owner, err := store.ObjectOwnerActorID(ctx, pool, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.UpsertFederatedObject(ctx, pool, id, row.ActorID, typ, rawJSON)
		}
		return err
	}
	if owner != row.ActorID {
		return fmt.Errorf("update: actor does not own object")
	}
	return store.UpsertFederatedObject(ctx, pool, id, row.ActorID, typ, rawJSON)
}

func handleDelete(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	rawObj, ok := fields["object"]
	if !ok {
		return fmt.Errorf("delete missing object")
	}
	targetID, err := as2.TombstoneOrObjectID(rawObj)
	if err != nil {
		return err
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		return nil
	}
	actorIRI, err := as2.ActorIRIFromActivity(fields)
	if err != nil {
		return err
	}
	signerURL, err := actorURLByDBID(ctx, pool, row.ActorID)
	if err != nil {
		return err
	}
	if !actorsMatch(actorIRI, signerURL) {
		return fmt.Errorf("delete: activity actor does not match stored actor")
	}
	owner, err := store.ObjectOwnerActorID(ctx, pool, targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if owner != row.ActorID {
		return fmt.Errorf("delete: actor does not own object")
	}
	return store.SoftDeleteFederatedObjectByURL(ctx, pool, targetID)
}

func actorsMatch(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}
