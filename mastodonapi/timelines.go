package mastodonapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
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
	rows = s.filterActivityRowsForViewer(ctx, 0, rows, "public")
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		st, ok := s.mastodonStatusPresentation(ctx, row, 0)
		if ok {
			out = append(out, st)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) getTimelineList(w http.ResponseWriter, r *http.Request, viewerActorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	raw := r.PathValue("id")
	listID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || listID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	base, err := url.Parse(strings.TrimSpace(s.cfg().PublicBaseURL))
	if err != nil || base.Hostname() == "" {
		writeAPIError(w, http.StatusInternalServerError, "instance URL not configured")
		return
	}
	ctx := r.Context()
	members, err := store.ListMastodonListMemberActorIDs(ctx, s.Pool, listID, viewerActorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	rows, err := store.ListRecentCreatesForMemberActors(ctx, s.Pool, members, base.Hostname(), timelineLimit(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load timeline")
		return
	}
	rows = s.filterActivityRowsForViewer(ctx, viewerActorID, rows, "home")
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		st, ok := s.mastodonStatusPresentation(ctx, row, viewerActorID)
		if ok {
			out = append(out, st)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) filterActivityRowsForViewer(ctx context.Context, viewerActorID int64, rows []store.ActivityRow, filterCtx string) []store.ActivityRow {
	if s.Pool == nil || viewerActorID < 1 {
		return rows
	}
	filters, err := store.ListMastodonFiltersForActor(ctx, s.Pool, viewerActorID)
	if err != nil || len(filters) == 0 {
		return rows
	}
	out := make([]store.ActivityRow, 0, len(rows))
	for _, row := range rows {
		var act map[string]any
		if err := json.Unmarshal(row.RawJSON, &act); err != nil {
			continue
		}
		note, _ := act["object"].(map[string]any)
		content, _ := note["content"].(string)
		if statusHiddenByFilters(content, filters, filterCtx) {
			continue
		}
		out = append(out, row)
	}
	return out
}
