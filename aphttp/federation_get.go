package aphttp

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

// prefersHTMLCatchall reports whether the client likely wants a browser page rather than ActivityPub JSON.
func prefersHTMLCatchall(r *http.Request) bool {
	acc := strings.ToLower(r.Header.Get("Accept"))
	if acc == "" {
		return true
	}
	if strings.Contains(acc, "text/html") &&
		!strings.Contains(acc, "application/json") &&
		!strings.Contains(acc, "application/activity+json") &&
		!strings.Contains(acc, "application/ld+json") {
		return true
	}
	return false
}

func (h *Handler) publicHostname() string {
	u, err := url.Parse(strings.TrimSpace(h.cfg.PublicBaseURL))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// serveCatchallHostHTML is a minimal HTML response with the instance hostname (unknown paths for browsers).
func (h *Handler) serveCatchallHostHTML(w http.ResponseWriter) {
	host := h.publicHostname()
	if host == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	esc := html.EscapeString(host)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><h1>%s</h1>`,
		esc, esc)
}

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
//
// GET / is delegated to GetRoot (cannot register both GET / and GET /{path...} on Go 1.22+ ServeMux).
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
	p := r.URL.Path
	if p == "" {
		p = "/"
	}
	if p == "/" {
		h.GetRoot(w, r)
		return
	}
	if h.st == nil || strings.TrimSpace(h.cfg.PublicBaseURL) == "" {
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

	noteIRI, actURL, err := store.DeletedCreateNoteForFederationGET(ctx, h.st.Pool, candidates)
	if err != nil {
		http.Error(w, "deleted activity lookup", http.StatusInternalServerError)
		return
	}
	if noteIRI != "" {
		if _, ok := h.cfg.LocalUsernameForActorURL(actURL); !ok {
			http.NotFound(w, r)
			return
		}
		writeTombstoneGone(w, r, noteIRI)
		return
	}

	objURL, ownerURL, err := store.DeletedObjectForFederationGET(ctx, h.st.Pool, candidates)
	if err != nil {
		http.Error(w, "deleted object lookup", http.StatusInternalServerError)
		return
	}
	if objURL != "" {
		if _, ok := h.cfg.LocalUsernameForActorURL(ownerURL); !ok {
			http.NotFound(w, r)
			return
		}
		writeTombstoneGone(w, r, objURL)
		return
	}

	if prefersHTMLCatchall(r) {
		h.serveCatchallHostHTML(w)
		return
	}
	http.NotFound(w, r)
}

func writeActivityStreamsJSON(w http.ResponseWriter, r *http.Request, raw []byte) {
	writeActivityStreamsJSONHeader(w, r)
	_, _ = w.Write(raw)
}

func writeTombstoneGone(w http.ResponseWriter, r *http.Request, objectIRI string) {
	objectIRI = strings.TrimSpace(objectIRI)
	if objectIRI == "" {
		http.NotFound(w, r)
		return
	}
	body, err := json.Marshal(map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"type":     "Tombstone",
		"id":       objectIRI,
	})
	if err != nil {
		http.Error(w, "tombstone", http.StatusInternalServerError)
		return
	}
	writeActivityStreamsJSONHeader(w, r)
	w.WriteHeader(http.StatusGone)
	_, _ = w.Write(body)
}

func writeActivityStreamsJSONHeader(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/ld+json") || strings.Contains(accept, "application/activity+json") {
		w.Header().Set("Content-Type", "application/activity+json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"; charset=utf-8")
	}
}
