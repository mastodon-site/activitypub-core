package mastodonapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

// --- Accounts ---

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	m, err := s.accountMap(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONResponse(w, http.StatusOK, m)
}

func (s *Server) patchAccountUpdateCredentials(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPatch {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	m, err := s.accountMap(r.Context(), actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONResponse(w, http.StatusOK, m)
}

func (s *Server) getAccountRelationships(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ids := r.URL.Query()["id[]"]
	if len(ids) == 0 {
		for _, v := range r.URL.Query()["id"] {
			ids = append(ids, v)
		}
	}
	out := make([]any, 0, len(ids))
	for _, raw := range ids {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		out = append(out, map[string]any{
			"id":                   raw,
			"following":            false,
			"requested":            false,
			"endorsed":             false,
			"followed_by":          false,
			"muting":               false,
			"muting_notifications": false,
			"showing_reblogs":      true,
			"blocking":             false,
			"blocked_by":           false,
			"domain_blocking":      false,
			"note":                 "",
		})
	}
	writeJSONResponse(w, http.StatusOK, out)
}

func (s *Server) getAccountLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	acct := strings.TrimSpace(r.URL.Query().Get("acct"))
	if acct == "" {
		writeAPIError(w, http.StatusBadRequest, "acct is required")
		return
	}
	u := *r.URL
	q := u.Query()
	q.Set("q", acct)
	u.RawQuery = q.Encode()
	req2 := r.Clone(r.Context())
	req2.URL = &u
	rr := httptest.NewRecorder()
	s.getAccountSearch(rr, req2)
	if rr.Code != http.StatusOK {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	var accounts []any
	if err := json.Unmarshal(rr.Body.Bytes(), &accounts); err != nil || len(accounts) == 0 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONResponse(w, http.StatusOK, accounts[0])
}

// --- Statuses ---

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	raw := r.PathValue("id")
	dbID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || dbID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	row, err := store.GetActivityByID(r.Context(), s.Pool, dbID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(row.Type), "create") {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	st, ok := s.mastodonStatusFromCreateRow(r.Context(), *row)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONResponse(w, http.StatusOK, st)
}

func (s *Server) getStatusContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool != nil {
		raw := r.PathValue("id")
		dbID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || dbID < 1 {
			writeAPIError(w, http.StatusNotFound, "Record not found")
			return
		}
		if _, err := store.GetActivityByID(r.Context(), s.Pool, dbID); err != nil {
			writeAPIError(w, http.StatusNotFound, "Record not found")
			return
		}
	}
	writeJSONObjectOK(w, map[string]any{
		"ancestors":   []any{},
		"descendants": []any{},
	})
}

func (s *Server) deleteStatus(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

func (s *Server) postStatusActionNotFound(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeAPIError(w, http.StatusNotFound, "Record not found")
}

// --- Search ---

func (s *Server) getSearchV1(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSONObjectOK(w, map[string]any{"accounts": []any{}, "statuses": []any{}, "hashtags": []any{}})
		return
	}
	typ := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("type")))
	if typ == "" || typ == "accounts" {
		u := *r.URL
		qry := u.Query()
		qry.Set("q", q)
		u.RawQuery = qry.Encode()
		req2 := r.Clone(r.Context())
		req2.URL = &u
		rr := httptest.NewRecorder()
		s.getAccountSearch(rr, req2)
		if rr.Code == http.StatusOK {
			var accounts []any
			_ = json.Unmarshal(rr.Body.Bytes(), &accounts)
			if accounts == nil {
				accounts = []any{}
			}
			writeJSONObjectOK(w, map[string]any{
				"accounts": accounts,
				"statuses": []any{},
				"hashtags": []any{},
			})
			return
		}
	}
	writeJSONObjectOK(w, map[string]any{"accounts": []any{}, "statuses": []any{}, "hashtags": []any{}})
}
