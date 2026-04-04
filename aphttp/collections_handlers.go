package aphttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

// GetFollowersCollection serves GET /{handle}/followers
func (h *Handler) GetFollowersCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" || h.st == nil || len(h.localActorIDs) == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	handle := r.PathValue("handle")
	username, ok := parseAtHandle(handle)
	if !ok || !h.IsLocalActor(username) {
		http.NotFound(w, r)
		return
	}
	actorID, ok := h.localActorIDs[username]
	if !ok || actorID == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	limit, maxID, sinceID := collectionPageParams(r)
	total, items, nextCur, err := store.FollowersPage(r.Context(), h.st.Pool, actorID, limit, maxID, sinceID)
	if err != nil {
		http.Error(w, "followers", http.StatusInternalServerError)
		return
	}
	collID := h.cfg.LocalActorFollowersURL(username)
	if q := r.URL.RawQuery; q != "" {
		collID = collID + "?" + q
	}
	doc := orderedCollectionPageDoc(collID, h.cfg.LocalActorFollowersURL(username), total, items, nextCur, r)
	writeAS2JSON(w, r, doc)
}

// GetFollowingCollection serves GET /{handle}/following
func (h *Handler) GetFollowingCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" || h.st == nil || len(h.localActorIDs) == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	handle := r.PathValue("handle")
	username, ok := parseAtHandle(handle)
	if !ok || !h.IsLocalActor(username) {
		http.NotFound(w, r)
		return
	}
	actorID, ok := h.localActorIDs[username]
	if !ok || actorID == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	limit, maxID, sinceID := collectionPageParams(r)
	total, items, nextCur, err := store.FollowingPage(r.Context(), h.st.Pool, actorID, limit, maxID, sinceID)
	if err != nil {
		http.Error(w, "following", http.StatusInternalServerError)
		return
	}
	collID := h.cfg.LocalActorFollowingURL(username)
	if q := r.URL.RawQuery; q != "" {
		collID = collID + "?" + q
	}
	doc := orderedCollectionPageDoc(collID, h.cfg.LocalActorFollowingURL(username), total, items, nextCur, r)
	writeAS2JSON(w, r, doc)
}

func collectionPageParams(r *http.Request) (limit int, maxID, sinceID *int64) {
	q := r.URL.Query()
	limit = 50
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	maxID = parseInt64Ptr(q.Get("max_id"))
	sinceID = parseInt64Ptr(q.Get("since_id"))
	return limit, maxID, sinceID
}

func parseInt64Ptr(s string) *int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func orderedCollectionPageDoc(pageCollID, partOf string, total int64, items []string, nextCur *int64, r *http.Request) map[string]any {
	doc := map[string]any{
		"@context":     "https://www.w3.org/ns/activitystreams",
		"id":           pageCollID,
		"type":         "OrderedCollectionPage",
		"partOf":       partOf,
		"totalItems":   total,
		"orderedItems": items,
	}
	if nextCur != nil {
		u, err := url.Parse(r.URL.String())
		if err == nil {
			q := u.Query()
			q.Set("max_id", strconv.FormatInt(*nextCur, 10))
			u.RawQuery = q.Encode()
			doc["next"] = u.String()
		}
	}
	return doc
}
