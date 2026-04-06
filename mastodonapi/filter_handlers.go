package mastodonapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mastodon-site/activitypub-core/store"
)

func filterContexts(f store.MastodonFilter) []any {
	var c []any
	if f.ContextHome {
		c = append(c, "home")
	}
	if f.ContextNotifications {
		c = append(c, "notifications")
	}
	if f.ContextPublic {
		c = append(c, "public")
	}
	if f.ContextThread {
		c = append(c, "thread")
	}
	if f.ContextAccount {
		c = append(c, "account")
	}
	if len(c) == 0 {
		c = []any{"home"}
	}
	return c
}

func mastodonV1FilterJSON(f store.MastodonFilter) map[string]any {
	var exp any
	if f.ExpiresAt != nil {
		exp = f.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"id":           strconv.FormatInt(f.ID, 10),
		"phrase":       f.Phrase,
		"context":      filterContexts(f),
		"whole_word":   f.WholeWord,
		"expires_at":   exp,
		"irreversible": f.Irreversible,
	}
}

func (s *Server) getFiltersV1(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeJSONArrayOK(w, nil)
		return
	}
	filters, err := store.ListMastodonFiltersForActor(r.Context(), s.Pool, actorID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load filters")
		return
	}
	out := make([]any, 0, len(filters))
	for _, f := range filters {
		out = append(out, mastodonV1FilterJSON(f))
	}
	writeJSONArrayOK(w, out)
}

func (s *Server) getFiltersV2(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.getFiltersV1(w, r, actorID)
}

type filterPostJSON struct {
	Phrase               string   `json:"phrase"`
	WholeWord            bool     `json:"whole_word"`
	Irreversible         bool     `json:"irreversible"`
	ExpiresIn            *int64   `json:"expires_in"`
	Context              []string `json:"context"`
	ContextHome          *bool    `json:"context_home"`
	ContextNotifications *bool    `json:"context_notifications"`
	ContextPublic        *bool    `json:"context_public"`
	ContextThread        *bool    `json:"context_thread"`
	ContextAccount       *bool    `json:"context_account"`
}

func parseFilterPostBody(r *http.Request) (filterPostJSON, error) {
	var body filterPostJSON
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		return body, err
	}
	return body, nil
}

func contextsFromBody(b filterPostJSON) (ch, cn, cp, ct, ca bool) {
	if len(b.Context) > 0 {
		for _, c := range b.Context {
			switch strings.ToLower(strings.TrimSpace(c)) {
			case "home":
				ch = true
			case "notifications":
				cn = true
			case "public":
				cp = true
			case "thread":
				ct = true
			case "account":
				ca = true
			}
		}
		return ch, cn, cp, ct, ca
	}
	if b.ContextHome != nil {
		ch = *b.ContextHome
	}
	if b.ContextNotifications != nil {
		cn = *b.ContextNotifications
	}
	if b.ContextPublic != nil {
		cp = *b.ContextPublic
	}
	if b.ContextThread != nil {
		ct = *b.ContextThread
	}
	if b.ContextAccount != nil {
		ca = *b.ContextAccount
	}
	return ch, cn, cp, ct, ca
}

func (s *Server) postFilters(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	body, err := parseFilterPostBody(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	var exp *time.Time
	if body.ExpiresIn != nil && *body.ExpiresIn > 0 {
		t := time.Now().UTC().Add(time.Duration(*body.ExpiresIn) * time.Second)
		exp = &t
	}
	ch, cn, cp, ct, ca := contextsFromBody(body)
	id, err := store.InsertMastodonFilter(r.Context(), s.Pool, actorID, body.Phrase, body.WholeWord, body.Irreversible, exp, ch, cn, cp, ct, ca)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := store.GetMastodonFilter(r.Context(), s.Pool, id, actorID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load filter")
		return
	}
	writeJSONObjectOK(w, mastodonV1FilterJSON(*f))
}

func (s *Server) getFilterByID(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	fid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || fid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	f, err := store.GetMastodonFilter(r.Context(), s.Pool, fid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONObjectOK(w, mastodonV1FilterJSON(*f))
}

func (s *Server) putFilterByID(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	fid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || fid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	body, err := parseFilterPostBody(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	var exp *time.Time
	if body.ExpiresIn != nil && *body.ExpiresIn > 0 {
		t := time.Now().UTC().Add(time.Duration(*body.ExpiresIn) * time.Second)
		exp = &t
	}
	ch, cn, cp, ct, ca := contextsFromBody(body)
	if err := store.UpdateMastodonFilter(r.Context(), s.Pool, fid, actorID, body.Phrase, body.WholeWord, body.Irreversible, exp, ch, cn, cp, ct, ca); err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	f, err := store.GetMastodonFilter(r.Context(), s.Pool, fid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	writeJSONObjectOK(w, mastodonV1FilterJSON(*f))
}

func (s *Server) deleteFilterByID(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	fid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || fid < 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ok, err := store.DeleteMastodonFilter(r.Context(), s.Pool, fid, actorID)
	if err != nil || !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
