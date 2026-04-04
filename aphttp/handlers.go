// Package aphttp serves ActivityPub HTTP surfaces (WebFinger, actors, inboxes).
package aphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// Handler bundles AP HTTP handlers for mounting on the API mux.
type Handler struct {
	cfg *config.Config
}

// New creates AP HTTP handlers. cfg.PublicBaseURL must be set for meaningful responses.
func New(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

// WebFinger handles GET /.well-known/webfinger?resource=acct:user@host
func (h *Handler) WebFinger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("resource")
	if q == "" || !strings.HasPrefix(q, "acct:") {
		http.Error(w, "resource parameter required (acct:...)", http.StatusBadRequest)
		return
	}
	acct := strings.TrimPrefix(q, "acct:")
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured (AP_PUBLIC_BASE_URL)", http.StatusInternalServerError)
		return
	}
	base, err := url.Parse(h.cfg.PublicBaseURL)
	if err != nil {
		http.Error(w, "invalid AP_PUBLIC_BASE_URL", http.StatusInternalServerError)
		return
	}
	host := base.Hostname()
	user, domain, ok := strings.Cut(acct, "@")
	if !ok || user == "" || domain == "" {
		http.Error(w, "invalid acct", http.StatusBadRequest)
		return
	}
	if domain != host {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if user != h.cfg.LocalUsername {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	subject := fmt.Sprintf("acct:%s@%s", user, host)
	profile := strings.TrimRight(h.cfg.PublicBaseURL, "/") + "/users/" + url.PathEscape(user)
	resp := map[string]any{
		"subject": subject,
		"aliases": []string{profile},
		"links": []map[string]any{
			{
				"rel":  "self",
				"type": "application/activity+json",
				"href": profile,
			},
			{
				"rel":  "http://webfinger.net/rel/profile-page",
				"type": "text/html",
				"href": profile,
			},
		},
	}
	w.Header().Set("Content-Type", "application/jrd+json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetActor returns a minimal Actor for the local user (stub until DB-backed).
func (h *Handler) GetActor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured", http.StatusInternalServerError)
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/users/")
	username = strings.Trim(username, "/")
	if username != h.cfg.LocalUsername {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	base := strings.TrimRight(h.cfg.PublicBaseURL, "/")
	profile := base + "/users/" + url.PathEscape(username)
	inbox := base + "/inbox"
	outbox := base + "/outbox/" + url.PathEscape(username)
	// Placeholder key: real implementation loads from DB and signs with PEM.
	keyID := profile + "#main-key"
	actor := map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                profile,
		"type":              "Person",
		"preferredUsername": username,
		"inbox":             inbox,
		"outbox":            outbox,
		"publicKey": map[string]any{
			"id":           keyID,
			"owner":        profile,
			"type":         "key",
			"publicKeyPem": "-----BEGIN PUBLIC KEY-----\n(stub — replace with real RSA key from store)\n-----END PUBLIC KEY-----\n",
		},
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/ld+json") || strings.Contains(accept, "application/activity+json") {
		w.Header().Set("Content-Type", "application/activity+json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"; charset=utf-8")
	}
	_ = json.NewEncoder(w).Encode(actor)
}

// Placeholder inbox — Accept activity+json POST (verify in later milestone).
func (h *Handler) SharedInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// Mount registers routes on mux. basePath is typically empty (Host root).
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/webfinger", h.WebFinger)
	mux.HandleFunc("GET /users/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Count(strings.TrimSuffix(r.URL.Path, "/"), "/") > 1 {
			http.NotFound(w, r)
			return
		}
		h.GetActor(w, r)
	})
	mux.HandleFunc("POST /inbox", h.SharedInbox)
}

// Health is a no-op liveness handler.
func Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Ready pings the database when store is non-nil.
func Ready(store interface{ Ping(context.Context) error }) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if err := store.Ping(r.Context()); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
