package mastodonapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/mastodon-site/activitypub-core/store"
)

func (s *Server) getAccountStatuses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	rawID := r.PathValue("id")
	targetID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || targetID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	ctx := r.Context()
	rows, err := store.ListRecentCreateActivitiesForActor(ctx, s.Pool, targetID, timelineLimit(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load statuses")
		return
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		st, ok := s.mastodonStatusFromCreateRow(ctx, row)
		if ok {
			out = append(out, st)
		}
	}
	writeJSONArrayOK(w, out)
}

func (s *Server) getAccountFollowers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.writeAccountRelationPage(w, r, true)
}

func (s *Server) getAccountFollowing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.writeAccountRelationPage(w, r, false)
}

func (s *Server) writeAccountRelationPage(w http.ResponseWriter, r *http.Request, followers bool) {
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	rawID := r.PathValue("id")
	targetID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || targetID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	ctx := r.Context()
	limit := timelineLimit(r)
	var iris []string
	var ferr error
	if followers {
		_, iris, _, ferr = store.FollowersPage(ctx, s.Pool, targetID, limit, nil, nil)
	} else {
		_, iris, _, ferr = store.FollowingPage(ctx, s.Pool, targetID, limit, nil, nil)
	}
	if ferr != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load accounts")
		return
	}
	out, _ := s.accountMapsFromActorIRIs(ctx, iris)
	writeJSONArrayOK(w, out)
}

func (s *Server) accountMapsFromActorIRIs(ctx context.Context, iris []string) ([]any, error) {
	out := make([]any, 0, len(iris))
	for _, iri := range iris {
		id, err := store.ActorIDByActorURL(ctx, s.Pool, iri)
		if err != nil {
			continue
		}
		m, err := s.accountMap(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
