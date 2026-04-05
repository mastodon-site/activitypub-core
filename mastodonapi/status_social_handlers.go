package mastodonapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mastodon-site/activitypub-core/internal/as2"
	"github.com/mastodon-site/activitypub-core/store"
)

func (s *Server) postStatusFavourite(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.postStatusSocialLike(w, r, actorID, true)
}

func (s *Server) postStatusUnfavourite(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.postStatusSocialLike(w, r, actorID, false)
}

func (s *Server) postStatusSocialLike(w http.ResponseWriter, r *http.Request, actorID int64, favour bool) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	ctx := r.Context()
	uname, _, _, _, _, err := store.ActorForMastodon(ctx, s.Pool, actorID)
	if err != nil || !s.H.IsLocalActor(uname) {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	statusID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || statusID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	row, err := store.GetActivityByID(ctx, s.Pool, statusID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	res, ok := resolveCreateStatusRow(row)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	root := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	prof := s.cfg().LocalActorProfileURL(uname)

	if favour {
		exists, _ := store.ActorHasLikedObject(ctx, s.Pool, actorID, res.NoteIRI)
		if exists {
			s.writeStatusPresentation(w, ctx, res.Row, actorID)
			return
		}
		likeID := newIRI(root, "activities")
		if res.AuthorIRI == "" {
			writeAPIError(w, http.StatusBadRequest, "cannot resolve note author for like addressing")
			return
		}
		like := map[string]any{
			"@context": "https://www.w3.org/ns/activitystreams",
			"type":     "Like",
			"id":       likeID,
			"actor":    prof,
			"object":   res.NoteIRI,
			"to":       []string{res.AuthorIRI},
		}
		raw, err := json.Marshal(like)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "could not build activity")
			return
		}
		if err := s.H.PublishLocalActivityBytes(ctx, uname, raw); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		likeAct, err := store.FederatedLikeActivityID(ctx, s.Pool, actorID, res.NoteIRI)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAPIError(w, http.StatusNotFound, "Record not found")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "could not load like")
			return
		}
		undoID := newIRI(root, "activities")
		undo := map[string]any{
			"@context": "https://www.w3.org/ns/activitystreams",
			"type":     "Undo",
			"id":       undoID,
			"actor":    prof,
			"object":   likeAct,
		}
		raw, err := json.Marshal(undo)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "could not build activity")
			return
		}
		if err := s.H.PublishLocalActivityBytes(ctx, uname, raw); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.writeStatusPresentation(w, ctx, res.Row, actorID)
}

func (s *Server) postStatusReblog(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.postStatusSocialReblog(w, r, actorID, true)
}

func (s *Server) postStatusUnreblog(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.postStatusSocialReblog(w, r, actorID, false)
}

func (s *Server) postStatusSocialReblog(w http.ResponseWriter, r *http.Request, actorID int64, reblog bool) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	ctx := r.Context()
	uname, _, _, _, _, err := store.ActorForMastodon(ctx, s.Pool, actorID)
	if err != nil || !s.H.IsLocalActor(uname) {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	statusID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || statusID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	row, err := store.GetActivityByID(ctx, s.Pool, statusID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	res, ok := resolveCreateStatusRow(row)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	root := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	prof := s.cfg().LocalActorProfileURL(uname)
	followers := s.cfg().LocalActorFollowersURL(uname)

	if reblog {
		exists, _ := store.ActorHasAnnouncedObject(ctx, s.Pool, actorID, res.NoteIRI)
		if exists {
			s.writeStatusPresentation(w, ctx, res.Row, actorID)
			return
		}
		annID := newIRI(root, "activities")
		cc := []string{followers}
		if res.AuthorIRI != "" {
			cc = append(cc, res.AuthorIRI)
		}
		ann := map[string]any{
			"@context":  []any{"https://www.w3.org/ns/activitystreams"},
			"type":      "Announce",
			"id":        annID,
			"actor":     prof,
			"published": time.Now().UTC().Format(time.RFC3339),
			"object":    res.NoteIRI,
			"to":        []string{"https://www.w3.org/ns/activitystreams#Public"},
			"cc":        cc,
		}
		raw, err := json.Marshal(ann)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "could not build activity")
			return
		}
		if err := s.H.PublishLocalActivityBytes(ctx, uname, raw); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		annAct, err := store.FederatedAnnounceActivityID(ctx, s.Pool, actorID, res.NoteIRI)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAPIError(w, http.StatusNotFound, "Record not found")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "could not load announce")
			return
		}
		undoID := newIRI(root, "activities")
		undo := map[string]any{
			"@context": "https://www.w3.org/ns/activitystreams",
			"type":     "Undo",
			"id":       undoID,
			"actor":    prof,
			"object":   annAct,
		}
		raw, err := json.Marshal(undo)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "could not build activity")
			return
		}
		if err := s.H.PublishLocalActivityBytes(ctx, uname, raw); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.writeStatusPresentation(w, ctx, res.Row, actorID)
}

func (s *Server) postStatusBookmark(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.postStatusBookmarkToggle(w, r, actorID, true)
}

func (s *Server) postStatusUnbookmark(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.postStatusBookmarkToggle(w, r, actorID, false)
}

func (s *Server) postStatusBookmarkToggle(w http.ResponseWriter, r *http.Request, actorID int64, add bool) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	ctx := r.Context()
	statusID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || statusID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	row, err := store.GetActivityByID(ctx, s.Pool, statusID)
	if err != nil || !strings.EqualFold(strings.TrimSpace(row.Type), "create") {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	if add {
		if err := store.UpsertStatusBookmark(ctx, s.Pool, actorID, statusID); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "could not bookmark")
			return
		}
	} else {
		if _, err := store.DeleteStatusBookmark(ctx, s.Pool, actorID, statusID); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "could not unbookmark")
			return
		}
	}
	s.writeStatusPresentation(w, ctx, *row, actorID)
}

func (s *Server) writeStatusPresentation(w http.ResponseWriter, ctx context.Context, row store.ActivityRow, viewer int64) {
	st, ok := s.mastodonStatusPresentation(ctx, row, viewer)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONResponse(w, http.StatusOK, st)
}

func (s *Server) getFavourites(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	ctx := r.Context()
	likes, err := store.ListRecentLikeActivitiesForActor(ctx, s.Pool, actorID, timelineLimit(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load favourites")
		return
	}
	out := make([]any, 0, len(likes))
	for _, likeRow := range likes {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(likeRow.RawJSON, &fields); err != nil {
			continue
		}
		objIRI, err := as2.ObjectIRI(fields)
		if err != nil {
			continue
		}
		createRow, err := store.FindCreateActivityByObjectNoteIRI(ctx, s.Pool, objIRI)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			continue
		}
		st, ok := s.mastodonStatusPresentation(ctx, *createRow, actorID)
		if ok {
			st["favourited"] = true
			out = append(out, st)
		}
	}
	writeJSONArrayOK(w, out)
}

func (s *Server) getBookmarks(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	ctx := r.Context()
	ids, err := store.ListBookmarkedStatusActivityIDs(ctx, s.Pool, actorID, timelineLimit(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load bookmarks")
		return
	}
	out := make([]any, 0, len(ids))
	for _, sid := range ids {
		row, err := store.GetActivityByID(ctx, s.Pool, sid)
		if err != nil {
			continue
		}
		st, ok := s.mastodonStatusPresentation(ctx, *row, actorID)
		if ok {
			st["bookmarked"] = true
			out = append(out, st)
		}
	}
	writeJSONArrayOK(w, out)
}

func (s *Server) getStatusFavouritedBy(w http.ResponseWriter, r *http.Request) {
	s.getStatusSocialAccounts(w, r, true)
}

func (s *Server) getStatusRebloggedBy(w http.ResponseWriter, r *http.Request) {
	s.getStatusSocialAccounts(w, r, false)
}

func (s *Server) getStatusSocialAccounts(w http.ResponseWriter, r *http.Request, likes bool) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	ctx := r.Context()
	statusID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || statusID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	row, err := store.GetActivityByID(ctx, s.Pool, statusID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	res, ok := resolveCreateStatusRow(row)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	limit := timelineLimit(r)
	var ids []int64
	if likes {
		ids, err = store.ListActorIDsWhoLikedObject(ctx, s.Pool, res.NoteIRI, limit)
	} else {
		ids, err = store.ListActorIDsWhoAnnouncedObject(ctx, s.Pool, res.NoteIRI, limit)
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load accounts")
		return
	}
	out := make([]any, 0, len(ids))
	for _, aid := range ids {
		m, err := s.accountMap(ctx, aid)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	writeJSONArrayOK(w, out)
}
