package fetch

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
)

// PrepareActorGETRequest optionally signs the request using the instance actor key (AP_SIGN_GET).
func PrepareActorGETRequest(req *http.Request, cfg *config.Config) error {
	if cfg == nil || !cfg.SignOutboundGET {
		return nil
	}
	path := strings.TrimSpace(cfg.InstanceActorPrivateKeyPath)
	if path == "" {
		path = strings.TrimSpace(cfg.ActorPrivateKeyPath)
	}
	if path == "" {
		return fmt.Errorf("AP_SIGN_GET requires AP_INSTANCE_ACTOR_PRIVATE_KEY_PATH or AP_ACTOR_PRIVATE_KEY_PATH")
	}
	priv, err := actorkey.LoadPrivateKeyFromFile(path)
	if err != nil {
		return fmt.Errorf("instance get signer: %w", err)
	}
	return httpsig.SignGet(req, cfg.InstanceActorKeyID(), priv)
}
