package aphttp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// localActorJSON builds ActivityPub Person JSON for a local username (shared by HTTP handlers).
func localActorJSON(cfg *config.Config, username, actorPublicKeyPEM string) map[string]any {
	profile := cfg.LocalActorProfileURL(username)
	inbox := cfg.LocalActorInboxURL(username)
	sharedInbox := cfg.LocalSharedInboxURL()
	outbox := cfg.LocalActorOutboxURL(username)
	followers := cfg.LocalActorFollowersURL(username)
	following := cfg.LocalActorFollowingURL(username)
	keyID := profile + "#main-key"
	publicPEM := actorPublicKeyPEM
	if publicPEM == "" {
		publicPEM = "-----BEGIN PUBLIC KEY-----\n(stub — set AP_ACTOR_PRIVATE_KEY_PATH)\n-----END PUBLIC KEY-----\n"
	}
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                profile,
		"type":              "Person",
		"preferredUsername": username,
		"inbox":             inbox,
		"outbox":            outbox,
		"followers":         followers,
		"following":         following,
		"endpoints": map[string]any{
			"sharedInbox": sharedInbox,
		},
		"publicKey": map[string]any{
			"id":           keyID,
			"owner":        profile,
			"type":         "Key",
			"publicKeyPem": publicPEM,
		},
	}
}

func writeAS2JSON(w http.ResponseWriter, r *http.Request, doc map[string]any) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/ld+json") || strings.Contains(accept, "application/activity+json") {
		w.Header().Set("Content-Type", "application/activity+json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"; charset=utf-8")
	}
	_ = json.NewEncoder(w).Encode(doc)
}

func instanceActorJSON(cfg *config.Config, instancePublicKeyPEM string) map[string]any {
	id := cfg.InstanceActorIRI()
	keyID := cfg.InstanceActorKeyID()
	inbox := cfg.LocalSharedInboxURL()
	publicPEM := instancePublicKeyPEM
	if publicPEM == "" {
		publicPEM = "-----BEGIN PUBLIC KEY-----\n(stub — configure instance or actor private key)\n-----END PUBLIC KEY-----\n"
	}
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                id,
		"type":              "Application",
		"preferredUsername": instancePreferredUsername(cfg),
		"inbox":             inbox,
		"publicKey": map[string]any{
			"id":           keyID,
			"owner":        id,
			"type":         "Key",
			"publicKeyPem": publicPEM,
		},
	}
}

func instancePreferredUsername(cfg *config.Config) string {
	u, err := url.Parse(cfg.PublicBaseURL)
	if err != nil {
		return "instance"
	}
	host := u.Hostname()
	if host == "" {
		return "instance"
	}
	return strings.ReplaceAll(host, ".", "")
}
