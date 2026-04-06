package aphttp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

// GetLocalActor serves GET /{handle} when handle is @username
func (h *Handler) GetLocalActor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured", http.StatusInternalServerError)
		return
	}
	handle := r.PathValue("handle")
	username, ok := parseAtHandle(handle)
	if !ok || !h.IsLocalActor(username) {
		http.NotFound(w, r)
		return
	}
	writeAS2JSON(w, r, localActorJSON(h.cfg, username, h.localActorPublicKeyPEM(r.Context(), username)))
}

func (h *Handler) localActorPublicKeyPEM(ctx context.Context, username string) string {
	if h.st == nil {
		return h.actorPublicKeyPEM
	}
	u, err := url.Parse(strings.TrimSpace(h.cfg.PublicBaseURL))
	if err != nil || u.Hostname() == "" {
		return h.actorPublicKeyPEM
	}
	pem, err := store.ActorPublicKeyPEMForLocalUser(ctx, h.st.Pool, u.Hostname(), username)
	if err != nil || strings.TrimSpace(pem) == "" {
		return h.actorPublicKeyPEM
	}
	if pem == store.LocalActorPublicKeyPlaceholder {
		return h.actorPublicKeyPEM
	}
	return pem
}
