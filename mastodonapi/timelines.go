package mastodonapi

import (
	"encoding/json"
	"net/http"
)

// getTimelinePublic implements GET /api/v1/timelines/public (no auth).
// Mastodon clients poll this for the live feed; we return an empty list until a status index exists.
// Query parameters (limit, local, only_media, max_id, since_id, min_id) are accepted and ignored.
func (s *Server) getTimelinePublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	out := []any{}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
