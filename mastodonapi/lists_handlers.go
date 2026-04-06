package mastodonapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

func (s *Server) getLists(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	lists, err := store.ListMastodonLists(r.Context(), s.Pool, actorID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load lists")
		return
	}
	out := make([]any, 0, len(lists))
	for _, l := range lists {
		out = append(out, mastodonListJSON(l))
	}
	writeJSONArrayOK(w, out)
}

func mastodonListJSON(l store.MastodonList) map[string]any {
	return map[string]any{
		"id":             strconv.FormatInt(l.ID, 10),
		"title":          l.Title,
		"replies_policy": l.RepliesPolicy,
		"exclusive":      l.Exclusive,
	}
}

func (s *Server) postLists(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	title := ""
	ct := r.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(ct), "application/json") {
		var body struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err == nil {
			title = strings.TrimSpace(body.Title)
		}
	} else {
		_ = r.ParseForm()
		title = strings.TrimSpace(r.FormValue("title"))
	}
	id, err := store.InsertMastodonList(r.Context(), s.Pool, actorID, title)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not create list")
		return
	}
	l := store.MastodonList{ID: id, OwnerActorID: actorID, Title: title, RepliesPolicy: "list"}
	writeJSONObjectOK(w, mastodonListJSON(l))
}

func (s *Server) getListByID(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || lid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	l, err := store.GetMastodonList(r.Context(), s.Pool, lid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONObjectOK(w, mastodonListJSON(*l))
}

func (s *Server) putListByID(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || lid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpdateMastodonListTitle(r.Context(), s.Pool, lid, actorID, body.Title); err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	l, err := store.GetMastodonList(r.Context(), s.Pool, lid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONObjectOK(w, mastodonListJSON(*l))
}

func (s *Server) deleteListByID(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || lid < 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ok, err := store.DeleteMastodonList(r.Context(), s.Pool, lid, actorID)
	if err != nil || !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getListAccounts(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || lid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	ids, err := store.ListMastodonListMemberActorIDs(r.Context(), s.Pool, lid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	out := make([]any, 0, len(ids))
	ctx := r.Context()
	for _, id := range ids {
		m, err := s.accountMap(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	writeJSONArrayOK(w, out)
}

func (s *Server) postListAccountsAdd(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || lid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	var body struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	for _, aid := range body.AccountIDs {
		if aid < 1 {
			continue
		}
		_ = store.AddMastodonListMember(r.Context(), s.Pool, lid, actorID, aid)
	}
	s.getListAccounts(w, r, actorID)
}

func (s *Server) deleteListAccountMember(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || lid < 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	aid, err := strconv.ParseInt(r.PathValue("account_id"), 10, 64)
	if err != nil || aid < 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err := store.RemoveMastodonListMember(r.Context(), s.Pool, lid, actorID, aid); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
