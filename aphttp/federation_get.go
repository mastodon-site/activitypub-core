package aphttp

import (
	"net/http"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

// canonicalIRICandidates builds possible ActivityPub IRIs for a request path using
// AP_PUBLIC_BASE_URL as origin (trailing slash variants).
func canonicalIRICandidates(publicBaseURL, requestPath string) []string {
	root := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if root == "" || requestPath == "" {
		return nil
	}
	p := requestPath
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	full := root + p
	add(full)
	if strings.HasSuffix(full, "/") {
		add(strings.TrimSuffix(full, "/"))
	} else {
		add(full + "/")
	}
	return out
}

// GetActivityOrObject serves GET for any path that matches a stored activity_id or object_url
// owned by a configured local actor. Register as the last GET route (e.g. GET /{path...}) so
// well-known, /@handle, /media, etc. remain authoritative.
func (h *Handler) GetActivityOrObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.RequireAuthorizedFetch {
		if err := h.verifyAuthorizedFetch(r); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	if h.st == nil || strings.TrimSpace(h.cfg.PublicBaseURL) == "" {
		http.NotFound(w, r)
		return
	}
	p := r.URL.Path
	if p == "" || p == "/" {
		http.NotFound(w, r)
		return
	}
	candidates := canonicalIRICandidates(h.cfg.PublicBaseURL, p)
	if len(candidates) == 0 {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	raw, actorURL, err := store.GetActivityJSONByIRIs(ctx, h.st.Pool, candidates)
	if err != nil {
		http.Error(w, "activity lookup", http.StatusInternalServerError)
		return
	}
	if raw != nil {
		if _, ok := h.cfg.LocalUsernameForActorURL(actorURL); !ok {
			http.NotFound(w, r)
			return
		}
		writeActivityStreamsJSON(w, r, raw)
		return
	}

	raw, actorURL, err = store.GetObjectJSONByIRIs(ctx, h.st.Pool, candidates)
	if err != nil {
		http.Error(w, "object lookup", http.StatusInternalServerError)
		return
	}
	if raw != nil {
		if _, ok := h.cfg.LocalUsernameForActorURL(actorURL); !ok {
			http.NotFound(w, r)
			return
		}
		writeActivityStreamsJSON(w, r, raw)
		return
	}
	http.NotFound(w, r)
}

func writeActivityStreamsJSON(w http.ResponseWriter, r *http.Request, raw []byte) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/ld+json") || strings.Contains(accept, "application/activity+json") {
		w.Header().Set("Content-Type", "application/activity+json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"; charset=utf-8")
	}
	_, _ = w.Write(raw)
}
