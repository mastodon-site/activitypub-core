package aphttp

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/mastodon-site/activitypub-core/store"
)

// GetOutbox serves GET /{handle}/outbox
func (h *Handler) GetOutbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" || h.st == nil || len(h.localActorIDs) == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	if !h.requireAuthorizedFetch(w, r) {
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
	collBase := h.cfg.LocalActorOutboxURL(username)
	if !collectionPagingActive(r) {
		total, _, _, err := store.OutboxPage(r.Context(), h.st.Pool, actorID, 1, nil, nil)
		if err != nil {
			http.Error(w, "outbox", http.StatusInternalServerError)
			return
		}
		doc := map[string]any{
			"@context":   "https://www.w3.org/ns/activitystreams",
			"id":         collBase,
			"type":       "OrderedCollection",
			"totalItems": total,
			"first":      firstCollectionPageURL(collBase, limit),
		}
		writeAS2JSON(w, r, doc)
		return
	}

	total, items, nextCur, err := store.OutboxPage(r.Context(), h.st.Pool, actorID, limit, maxID, sinceID)
	if err != nil {
		http.Error(w, "outbox", http.StatusInternalServerError)
		return
	}
	collID := collBase
	if q := r.URL.RawQuery; q != "" {
		collID = collBase + "?" + q
	}
	doc := map[string]any{
		"@context":     "https://www.w3.org/ns/activitystreams",
		"id":           collID,
		"type":         "OrderedCollectionPage",
		"partOf":       collBase,
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
	writeAS2JSON(w, r, doc)
}
