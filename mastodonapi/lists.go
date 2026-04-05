package mastodonapi

import (
	"encoding/json"
	"net/http"
)

// getLists implements GET /api/v1/lists (Bearer). Mastodon clients expect a JSON array; we have no
// list feature yet so the response is empty.
func (s *Server) getLists(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_ = actorID
	out := []any{}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
