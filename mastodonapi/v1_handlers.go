package mastodonapi

import (
	"context"
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
	if row.DeletedAt != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(row.Type), "create") {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	st, ok := s.mastodonStatusPresentation(r.Context(), *row, 0)
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
	if s.Pool == nil {
		writeJSONObjectOK(w, map[string]any{"ancestors": []any{}, "descendants": []any{}})
		return
	}
	ctx := r.Context()
	raw := r.PathValue("id")
	dbID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || dbID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	row, err := store.GetActivityByID(ctx, s.Pool, dbID)
	if err != nil || row.DeletedAt != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	res, ok := resolveCreateStatusRow(row)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	ancRows := s.ancestorCreateChain(ctx, row)
	descRows, err := store.ListCreateActivitiesReplyingToNoteIRI(ctx, s.Pool, res.NoteIRI, 80)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load context")
		return
	}
	viewer := int64(0)
	if aid, ok := s.actorIDFromBearer(r); ok {
		viewer = aid
	}
	ancestors := make([]any, 0, len(ancRows))
	for _, ar := range ancRows {
		st, ok := s.mastodonStatusPresentation(ctx, ar, viewer)
		if ok {
			ancestors = append(ancestors, st)
		}
	}
	descendants := make([]any, 0, len(descRows))
	for _, dr := range descRows {
		st, ok := s.mastodonStatusPresentation(ctx, dr, viewer)
		if ok {
			descendants = append(descendants, st)
		}
	}
	writeJSONObjectOK(w, map[string]any{
		"ancestors":   ancestors,
		"descendants": descendants,
	})
}

func (s *Server) ancestorCreateChain(ctx context.Context, start *store.ActivityRow) []store.ActivityRow {
	if s.Pool == nil || start == nil {
		return nil
	}
	var chain []store.ActivityRow
	cur := start
	seen := map[int64]struct{}{}
	for i := 0; i < 64; i++ {
		res, ok := resolveCreateStatusRow(cur)
		if !ok {
			break
		}
		parentIRI, _ := res.Note["inReplyTo"].(string)
		parentIRI = strings.TrimSpace(parentIRI)
		if parentIRI == "" {
			break
		}
		parent, err := store.FindCreateActivityByObjectNoteIRI(ctx, s.Pool, parentIRI)
		if err != nil || parent == nil || parent.DeletedAt != nil {
			break
		}
		if _, dup := seen[parent.ID]; dup {
			break
		}
		seen[parent.ID] = struct{}{}
		chain = append([]store.ActivityRow{*parent}, chain...)
		cur = parent
	}
	return chain
}

func (s *Server) deleteStatus(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	ctx := r.Context()
	raw := r.PathValue("id")
	dbID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || dbID < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	row, err := store.GetActivityByID(ctx, s.Pool, dbID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	if row.ActorID != actorID {
		writeAPIError(w, http.StatusForbidden, "not allowed")
		return
	}
	if row.DeletedAt != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	res, ok := resolveCreateStatusRow(row)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	uname, _, _, _, _, err := store.ActorForMastodon(ctx, s.Pool, actorID)
	if err != nil || !s.H.IsLocalActor(uname) {
		writeAPIError(w, http.StatusForbidden, "not a local account")
		return
	}
	st, ok := s.mastodonStatusPresentation(ctx, *row, actorID)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "could not build status")
		return
	}
	root := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	prof := s.cfg().LocalActorProfileURL(uname)
	del := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"type":     "Delete",
		"id":       newIRI(root, "activities"),
		"actor":    prof,
		"to":       []string{"https://www.w3.org/ns/activitystreams#Public"},
		"object":   res.NoteIRI,
	}
	rawDel, err := json.Marshal(del)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not build delete")
		return
	}
	if err := s.H.PublishLocalActivityBytes(ctx, uname, rawDel); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := store.SoftDeleteActivityForActor(ctx, s.Pool, dbID, actorID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	st["content"] = ""
	st["text"] = ""
	st["media_attachments"] = []any{}
	writeJSONResponse(w, http.StatusOK, st)
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
