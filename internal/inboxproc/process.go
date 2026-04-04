// Package inboxproc applies side effects for activities after they are persisted and queued.
package inboxproc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/as2"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/store"
)

// DeliverPayload must match cmd/apw deliverActivity JSON shape.
type DeliverPayload struct {
	InboxURL        string          `json:"inboxUrl"`
	Body            json.RawMessage `json:"body"`
	LocalUsername   string          `json:"localUsername,omitempty"`
	SigningUsername string          `json:"signingUsername,omitempty"` // alias for worker compatibility
}

// ProcessInboxActivity loads an activity by DB id and runs type-specific handlers.
// fetchPolicy overrides outbound URL policy when non-nil (tests); production callers pass nil.
func ProcessInboxActivity(ctx context.Context, pool *pgxpool.Pool, q queue.Backend, cfg *config.Config, httpClient *http.Client, activityDBID int64, fetchPolicy *fetch.Policy) error {
	row, err := store.GetActivityByID(ctx, pool, activityDBID)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(row.RawJSON, &fields); err != nil {
		return fmt.Errorf("decode activity json: %w", err)
	}

	t := activityTypeNormalized(row.Type)
	pol := fetchPolicy
	if pol == nil {
		pol = fetch.PolicyFromConfig(cfg)
	}
	switch {
	case strings.EqualFold(t, "Follow"):
		return handleFollow(ctx, pool, q, cfg, httpClient, pol, row, fields)
	case strings.EqualFold(t, "Accept"):
		return handleAccept(ctx, pool, row, fields)
	case strings.EqualFold(t, "Reject"):
		return handleReject(ctx, pool, row, fields)
	case strings.EqualFold(t, "Undo"):
		return handleUndo(ctx, pool, row, fields)
	default:
		return nil
	}
}

func activityTypeNormalized(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndex(t, "#"); i >= 0 {
		return t[i+1:]
	}
	return t
}

func handleFollow(ctx context.Context, pool *pgxpool.Pool, q queue.Backend, cfg *config.Config, httpClient *http.Client, fetchPolicy *fetch.Policy, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	objectIRI, err := as2.ObjectIRI(fields)
	if err != nil {
		return err
	}
	if _, ok := cfg.LocalUsernameForActorURL(objectIRI); !ok {
		return fmt.Errorf("follow object is not a local actor")
	}
	followeeID, err := store.ActorIDByActorURL(ctx, pool, objectIRI)
	if err != nil {
		return fmt.Errorf("followee actor: %w", err)
	}
	followerID := row.ActorID
	if err := store.UpsertFollow(ctx, pool, followerID, followeeID, row.ActivityID, store.FollowStatePending); err != nil {
		return err
	}
	if !cfg.FollowAutoAccept {
		return nil
	}
	var followeeActorURL string
	if err := pool.QueryRow(ctx, `SELECT actor_url FROM actors WHERE id = $1`, followeeID).Scan(&followeeActorURL); err != nil {
		return err
	}
	followeeUser, ok := cfg.LocalUsernameForActorURL(followeeActorURL)
	if !ok {
		return fmt.Errorf("followee id %d not local", followeeID)
	}
	var followerActorURL string
	if err := pool.QueryRow(ctx, `SELECT actor_url FROM actors WHERE id = $1`, followerID).Scan(&followerActorURL); err != nil {
		return err
	}
	inbox, err := fetch.InboxURLFromReference(ctx, httpClient, fetchPolicy, followerActorURL)
	if err != nil {
		return fmt.Errorf("follower inbox: %w", err)
	}
	acceptBody, err := buildAcceptFollow(cfg, followeeUser, followeeActorURL, row.ActivityID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(DeliverPayload{
		InboxURL:        inbox,
		Body:            acceptBody,
		LocalUsername:   followeeUser,
		SigningUsername: followeeUser,
	})
	if err != nil {
		return err
	}
	job := queue.Job{
		Type:           queue.TypeDeliverActivity,
		Payload:        payload,
		IdempotencyKey: "accept:" + row.ActivityID + ":" + inbox,
	}
	if err := q.Enqueue(ctx, job); err != nil {
		return err
	}
	return store.SetFollowStateByFollowActivityIDForFollower(ctx, pool, row.ActivityID, store.FollowStateAccepted, row.ActorID)
}

func buildAcceptFollow(cfg *config.Config, followeeUser, followeeProfile, followActivityIRI string) ([]byte, error) {
	_ = cfg
	_ = followeeUser
	frag := "req"
	if u, err := url.Parse(followActivityIRI); err == nil && strings.Trim(u.Path, "/") != "" {
		frag = strings.ReplaceAll(strings.Trim(u.Path, "/"), "/", "-")
	}
	acceptID := strings.TrimRight(followeeProfile, "/") + "/accepts/follows/" + url.PathEscape(frag)
	doc := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       acceptID,
		"type":     "Accept",
		"actor":    followeeProfile,
		"object":   followActivityIRI,
	}
	return json.Marshal(doc)
}

func handleAccept(ctx context.Context, pool *pgxpool.Pool, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	target, err := as2.ObjectIRI(fields)
	if err != nil {
		return err
	}
	return store.SetFollowStateByFollowActivityIDForFollower(ctx, pool, target, store.FollowStateAccepted, row.ActorID)
}

func handleReject(ctx context.Context, pool *pgxpool.Pool, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	target, err := as2.ObjectIRI(fields)
	if err != nil {
		return err
	}
	return store.SetFollowStateByFollowActivityIDForFollower(ctx, pool, target, store.FollowStateRejected, row.ActorID)
}

func handleUndo(ctx context.Context, pool *pgxpool.Pool, row *store.ActivityRow, fields map[string]json.RawMessage) error {
	raw, ok := fields["object"]
	if !ok {
		return fmt.Errorf("undo missing object")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return store.DeleteFollowByFollowActivityIDForFollower(ctx, pool, s, row.ActorID)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		if id, err := jsonStringFromMap(obj, "id"); err == nil {
			return deleteFollowForUndoTarget(ctx, pool, row.ActorID, obj, id)
		}
	}
	return fmt.Errorf("undo object shape not supported")
}

func jsonStringFromMap(m map[string]json.RawMessage, key string) (string, error) {
	raw, ok := m[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return "", fmt.Errorf("invalid %s", key)
	}
	return s, nil
}

func deleteFollowForUndoTarget(ctx context.Context, pool *pgxpool.Pool, followerActorID int64, obj map[string]json.RawMessage, id string) error {
	t := ""
	if raw, ok := obj["type"]; ok {
		var ts string
		if json.Unmarshal(raw, &ts) == nil {
			t = activityTypeNormalized(ts)
		}
	}
	if strings.EqualFold(t, "Follow") {
		return store.DeleteFollowByFollowActivityIDForFollower(ctx, pool, id, followerActorID)
	}
	return nil
}
