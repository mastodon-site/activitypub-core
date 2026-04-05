package mastodonapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

// getTimelinePublic implements GET /api/v1/timelines/public (no auth).
// Returns recent Create(Note) activities from actors on this instance (by actors.domain).
func (s *Server) getTimelinePublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		// Contract / lightweight mounts: no DB still exposes Mastodon-shaped empty timeline.
		writeJSONArrayOK(w, nil)
		return
	}
	base, err := url.Parse(strings.TrimSpace(s.cfg().PublicBaseURL))
	if err != nil || base.Hostname() == "" {
		writeAPIError(w, http.StatusInternalServerError, "instance URL not configured")
		return
	}
	ctx := r.Context()
	rows, err := store.ListRecentPublicCreateActivities(ctx, s.Pool, base.Hostname(), timelineLimit(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load timeline")
		return
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		st, ok := s.mastodonStatusFromCreateRow(ctx, row)
		if ok {
			out = append(out, st)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
