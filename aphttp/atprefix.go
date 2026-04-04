package aphttp

import (
	"net/http"
	"net/url"
	"strings"
)

// WithAtPaths dispatches /@username, /@username/outbox, /@username/followers, /@username/following before next.
func (h *Handler) WithAtPaths(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.tryServeAtPath(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) tryServeAtPath(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/@") {
		return false
	}
	rest := strings.TrimPrefix(p, "/@")
	if rest == "" {
		http.NotFound(w, r)
		return true
	}
	userPart, subpath, hasSub := strings.Cut(rest, "/")
	username, err := url.PathUnescape(userPart)
	if err != nil || username == "" {
		http.NotFound(w, r)
		return true
	}
	handle := "@" + username
	if hasSub && subpath != "" {
		subSeg, remainder, hasMore := strings.Cut(subpath, "/")
		if hasMore && remainder != "" {
			http.NotFound(w, r)
			return true
		}
		switch {
		case subSeg == "outbox" && r.Method == http.MethodGet:
			h.GetOutbox(w, cloneRequestWithPathValue(r, "handle", handle))
			return true
		case subSeg == "outbox" && r.Method == http.MethodPost:
			h.PostOutbox(w, cloneRequestWithPathValue(r, "handle", handle))
			return true
		case subSeg == "followers" && r.Method == http.MethodGet:
			h.GetFollowersCollection(w, cloneRequestWithPathValue(r, "handle", handle))
			return true
		case subSeg == "following" && r.Method == http.MethodGet:
			h.GetFollowingCollection(w, cloneRequestWithPathValue(r, "handle", handle))
			return true
		default:
			http.NotFound(w, r)
			return true
		}
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	h.GetLocalActor(w, cloneRequestWithPathValue(r, "handle", handle))
	return true
}

func cloneRequestWithPathValue(r *http.Request, key, value string) *http.Request {
	c := r.Clone(r.Context())
	c.SetPathValue(key, value)
	return c
}
