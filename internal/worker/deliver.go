package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
	"github.com/mastodon-site/activitypub-core/store"
)

type deliverPayload struct {
	InboxURL        string          `json:"inboxUrl"`
	Body            json.RawMessage `json:"body"`
	LocalUsername   string          `json:"localUsername,omitempty"`
	SigningUsername string          `json:"signingUsername,omitempty"`
}

func signingUsernameForDelivery(p deliverPayload, cfg *config.Config) string {
	if strings.TrimSpace(p.SigningUsername) != "" {
		return strings.TrimSpace(p.SigningUsername)
	}
	if strings.TrimSpace(p.LocalUsername) != "" {
		return strings.TrimSpace(p.LocalUsername)
	}
	return cfg.LocalUsername
}

// DeliverActivity POSTs a signed Activity to an inbox (same behavior as apw deliver_activity jobs).
func DeliverActivity(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, client *http.Client, raw json.RawMessage) error {
	if cfg.ActorPrivateKeyPath == "" {
		return fmt.Errorf("AP_ACTOR_PRIVATE_KEY_PATH required for deliver_activity")
	}
	if cfg.PublicBaseURL == "" {
		return fmt.Errorf("AP_PUBLIC_BASE_URL required for deliver_activity (keyId)")
	}
	priv, err := actorkey.LoadPrivateKeyFromFile(cfg.ActorPrivateKeyPath)
	if err != nil {
		return err
	}
	var p deliverPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("deliver_activity payload: %w", err)
	}
	if p.InboxURL == "" || len(p.Body) == 0 {
		return fmt.Errorf("deliver_activity: inboxUrl and body required")
	}
	user := signingUsernameForDelivery(p, cfg)
	if user == "" {
		return fmt.Errorf("deliver_activity: signing user required")
	}
	base, err := url.Parse(cfg.PublicBaseURL)
	if err != nil {
		return fmt.Errorf("deliver_activity: public base url: %w", err)
	}
	domain := base.Hostname()
	if pool != nil {
		ok, err := store.LocalActorUsernameExists(ctx, pool, domain, user)
		if err != nil {
			return fmt.Errorf("deliver_activity: %w", err)
		}
		if !ok {
			return fmt.Errorf("deliver_activity: signing user %q is not a local account", user)
		}
	} else if !cfg.IsLocalUsername(user) {
		return fmt.Errorf("deliver_activity: signing user %q is not a configured local account", user)
	}
	keyID := cfg.LocalActorProfileURL(user) + "#main-key"
	req, err := httpsig.NewSignedPost(p.InboxURL, p.Body, keyID, priv)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	if client == nil {
		client = fetch.NewHTTPClient(cfg, 60*time.Second)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("delivery POST %s: %s", resp.Status, strings.TrimSpace(string(slurp)))
	}
	return nil
}
