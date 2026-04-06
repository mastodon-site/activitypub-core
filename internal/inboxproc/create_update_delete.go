package inboxproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/as2"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/store"
)

func handleCreate(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, row *store.ActivityRow, fields map[string]json.RawMessage, httpClient *http.Client, fetchPolicy *fetch.Policy) error {
	rawObj, ok := fields["object"]
	if !ok {
		return fmt.Errorf("create missing object")
	}
	id, typ, rawJSON, err := as2.ObjectFieldIDType(rawObj)
	if err != nil {
		return err
	}
	if len(rawJSON) == 0 && id != "" && httpClient != nil {
		maxB := int64(1 << 20)
		if cfg != nil && cfg.CreateObjectMaxFetchBytes > 0 {
			maxB = int64(cfg.CreateObjectMaxFetchBytes)
		}
		pol := fetchPolicy
		if pol == nil {
			pol = fetch.PolicyFromConfig(cfg)
		}
		body, ferr := fetch.FetchActivityPubJSON(ctx, httpClient, pol, id, cfg, maxB)
		if ferr != nil {
			return fmt.Errorf("create fetch object: %w", ferr)
		}
		id2, typ2, raw2, err := as2.ObjectFieldIDType(body)
		if err != nil {
			return fmt.Errorf("create fetched object: %w", err)
		}
		if !actorsMatch(id2, id) {
			return fmt.Errorf("create: fetched object id does not match IRI")
		}
		id, typ, rawJSON = id2, typ2, raw2
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
