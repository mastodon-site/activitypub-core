package mastodonapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

// --- Conversations (direct visibility statuses) ---

func (s *Server) getConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	aid, ok := s.actorIDFromBearer(r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "The access token is invalid")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	base, err := url.Parse(strings.TrimSpace(s.cfg().PublicBaseURL))
	if err != nil || base.Hostname() == "" {
		writeAPIError(w, http.StatusInternalServerError, "instance URL not configured")
		return
	}
	ctx := r.Context()
	rows, err := store.ListRecentDirectCreatesInvolvingActor(ctx, s.Pool, aid, base.Hostname(), 40)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load conversations")
		return
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		st, ok := s.mastodonStatusPresentation(ctx, row, aid)
		if !ok {
			continue
		}
		convID := fmt.Sprint(row.ID)
		acct, _ := st["account"].(map[string]any)
		participants := []any{}
		if acct != nil {
			participants = append(participants, acct)
		}
		out = append(out, map[string]any{
			"id":                   convID,
			"unread":               false,
			"accounts":             participants,
			"last_status":          st,
			"last_status_at":       st["created_at"],
			"participant_accounts": participants,
		})
	}
	writeJSONArrayOK(w, out)
}

func (s *Server) getConversationByID(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	sid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || sid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	ctx := r.Context()
	row, err := store.GetActivityByID(ctx, s.Pool, sid)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	var act map[string]any
	if json.Unmarshal(row.RawJSON, &act) != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	note, _ := act["object"].(map[string]any)
	vis, _ := note[noteVisibilityKey].(string)
	if strings.ToLower(strings.TrimSpace(vis)) != "direct" {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	involved := row.ActorID == actorID
	if raw, ok := note[noteDirectRecipientsKey].([]any); ok {
		for _, e := range raw {
			switch v := e.(type) {
			case float64:
				if int64(v) == actorID {
					involved = true
				}
			}
		}
	}
	if !involved {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	st, ok := s.mastodonStatusPresentation(ctx, *row, actorID)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	acct, _ := st["account"].(map[string]any)
	participants := []any{}
	if acct != nil {
		participants = append(participants, acct)
	}
	writeJSONObjectOK(w, map[string]any{
		"id":                   strconv.FormatInt(row.ID, 10),
		"unread":               false,
		"accounts":             participants,
		"last_status":          st,
		"last_status_at":       st["created_at"],
		"participant_accounts": participants,
	})
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) postConversationsReadBulk(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) postConversationReadByID(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

// --- Follow requests ---

func (s *Server) postFollowRequestAuthorize(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) postFollowRequestReject(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

// --- Reports ---

func (s *Server) postReports(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeJSONObjectOK(w, map[string]any{"id": "0"})
}

// --- Streaming ---

func (s *Server) getStreaming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotImplemented, "Streaming API is not implemented")
}

// --- Scheduled statuses ---

func (s *Server) getScheduledStatuses(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) postScheduledStatuses(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotImplemented, "Scheduled statuses are not implemented")
}

func (s *Server) getScheduledStatus(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) putScheduledStatus(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) deleteScheduledStatus(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

// --- Announcements ---

func (s *Server) postAnnouncementDismiss(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) deleteAnnouncementDismiss(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

// --- Other discovery ---

func (s *Server) getFamiliarFollowers(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) getDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) getSuggestionsV1(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

// --- Account endorsements / featured (POST stubs) ---

func (s *Server) postAccountPin(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotImplemented, "Pinned accounts are not implemented")
}

func (s *Server) postAccountUnpin(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotImplemented, "Pinned accounts are not implemented")
}

func (s *Server) postStatusTranslateStub(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotImplemented, "Translation is not implemented")
}

func (s *Server) postFeaturedTag(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeJSONObjectOK(w, map[string]any{"id": "0", "name": "", "url": "", "statuses_count": 0})
}

func (s *Server) deleteFeaturedTag(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{"name": "", "url": "", "statuses_count": 0})
}
