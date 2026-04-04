// Package inboxproc applies side effects for activities after they are persisted and queued.
package inboxproc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

	t := as2.PrimaryActivityType(fields)
	if t == "" {
		t = as2.NormalizeActivityType(row.Type)
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return fmt.Errorf("activity missing type")
	}

	pol := fetchPolicy
	if pol == nil {
		pol = fetch.PolicyFromConfig(cfg)
	}

	key := strings.ToLower(t)
	switch key {
	case "create":
		return handleCreate(ctx, pool, cfg, row, fields)
	case "update":
		return handleUpdate(ctx, pool, cfg, row, fields)
	case "delete":
		return handleDelete(ctx, pool, cfg, row, fields)
	case "follow":
		return handleFollow(ctx, pool, q, cfg, httpClient, pol, row, fields)
	case "accept":
		return handleAccept(ctx, pool, row, fields)
	case "reject":
		return handleReject(ctx, pool, row, fields)
	case "undo":
		return handleUndo(ctx, pool, row, fields)
	case "like":
		return handleLike(ctx, pool, cfg, row, fields)
	case "announce":
		return handleAnnounce(ctx, pool, cfg, row, fields)
	case "block":
		return handleBlock(ctx, pool, row, fields)
	case "add", "arrive", "dislike", "flag", "ignore", "invite", "join", "leave",
		"listen", "move", "offer", "question", "remove", "tentativeaccept", "tentativereject",
		"travel", "view":
		// Known ActivityStreams types we deliberately do not persist (groups, side effects,
		// or app-specific vocabularies). Extend with new handlers when product needs them.
		return nil
	default:
		// Future or extended types — fail open so one unknown verb does not block the worker.
		return nil
	}
}
