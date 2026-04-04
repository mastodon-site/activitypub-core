package aphttp

import (
	"net/http"
	"net/url"
	"strings"
)

// WithLegacy wraps next so legacy GET /users/{name}, GET/POST /outbox/{name} redirect to /@ paths before routing.
func (h *Handler) WithLegacy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.tryLegacyOutbox(w, r) {
			return
		}
		if h.tryLegacyUserProfile(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) tryLegacyOutbox(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return false
	}
	path := r.URL.Path
	const pfx = "/outbox/"
	if !strings.HasPrefix(path, pfx) {
		return false
	}
	rest := strings.TrimPrefix(path, pfx)
	if rest == "" || strings.Contains(rest, "/") {
		return false
	}
	username, err := url.PathUnescape(rest)
	if err != nil || username == "" {
		return false
	}
	if !h.IsLocalActor(username) {
		return false
	}
	http.Redirect(w, r, h.cfg.LocalActorOutboxURL(username), http.StatusPermanentRedirect)
	return true
}

func (h *Handler) tryLegacyUserProfile(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := r.URL.Path
	const pfx = "/users/"
	if !strings.HasPrefix(path, pfx) {
		return false
	}
	rest := strings.TrimPrefix(path, pfx)
	if rest == "" || strings.Contains(rest, "/") {
		return false
	}
	username, err := url.PathUnescape(rest)
	if err != nil || username == "" {
		return false
	}
	if !h.IsLocalActor(username) {
		return false
	}
	http.Redirect(w, r, h.cfg.LocalActorProfileURL(username), http.StatusPermanentRedirect)
	return true
}
