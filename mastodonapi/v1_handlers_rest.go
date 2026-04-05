package mastodonapi

import (
	"io"
	"net/http"
)

// --- Conversations (DM UI stubs) ---

func (s *Server) getConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) getConversationByID(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
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

// --- Filters ---

func (s *Server) postFilters(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeJSONObjectOK(w, map[string]any{
		"id":            "0",
		"title":         "",
		"context":       []any{},
		"expires_at":    nil,
		"filter_action": "warn",
		"keywords":      []any{},
		"statuses":      []any{},
	})
}

func (s *Server) getFilterByID(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) putFilterByID(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) deleteFilterByID(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
