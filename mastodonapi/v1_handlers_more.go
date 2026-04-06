package mastodonapi

import (
	"io"
	"net/http"
)

// --- Instance (additional v1) ---

func (s *Server) getInstancePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) getInstanceActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{"week": []any{}, "statuses": "0", "logins": "0", "registrations": "0"})
}

func (s *Server) getInstanceRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) getInstanceTranslationLanguages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) getInstanceDomainBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

// --- Markers ---

func (s *Server) getMarkers(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) postMarkers(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeJSONObjectOK(w, map[string]any{})
}

// --- Notifications ---

func (s *Server) getNotifications(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) getNotificationsUnreadCount(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{"count": 0})
}

func (s *Server) getNotificationByID(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) postNotificationsClear(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) postNotificationsDismiss(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) postNotificationDismissByID(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}

// --- Polls ---

func (s *Server) getPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) postPollVotes(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

// --- Push ---

func (s *Server) getPushSubscription(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) postPushSubscription(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotImplemented, "Push subscriptions are not implemented")
}

func (s *Server) putPushSubscription(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) deletePushSubscription(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONObjectOK(w, map[string]any{})
}
