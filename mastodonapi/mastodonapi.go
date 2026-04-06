// Package mastodonapi implements a minimal Mastodon client API subset (Ivory login, post, search, follow).
package mastodonapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/store"
)

// Server handles /api/v1/* and /oauth/* for Mastodon-compatible clients.
type Server struct {
	H    *aphttp.Handler
	Pool *pgxpool.Pool
}

// Mount registers Mastodon API and OAuth routes. No-op if any argument is nil.
func Mount(mux *http.ServeMux, h *aphttp.Handler, pool *pgxpool.Pool) {
	if mux == nil || h == nil || pool == nil {
		return
	}
	s := &Server{H: h, Pool: pool}
	s.mountMastodon(mux)
}

func (s *Server) cfg() *config.Config { return s.H.Config() }

// writeAPIError returns a Mastodon-style JSON error body (clients expect JSON, not text/plain).
func writeAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (s *Server) instanceHost() string {
	u, err := url.Parse(s.cfg().PublicBaseURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *Server) actorIDFromBearer(r *http.Request) (int64, bool) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return 0, false
	}
	tok := strings.TrimSpace(parts[1])
	if tok == "" {
		return 0, false
	}
	aid, _, _, err := store.ActorIDForAccessToken(r.Context(), s.Pool, tok)
	if err != nil || aid == 0 {
		return 0, false
	}
	return aid, true
}

func (s *Server) bearer(next func(http.ResponseWriter, *http.Request, int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aid, ok := s.actorIDFromBearer(r)
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "The access token is invalid")
			return
		}
		next(w, r, aid)
	}
}

func (s *Server) postApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name, redirectURIs, scopes, website, err := decodeAppsRegistrationRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(redirectURIs) == "" {
		writeAPIError(w, http.StatusBadRequest, "redirect_uris required")
		return
	}
	if scopes == "" {
		scopes = "read write"
	}
	app, err := store.InsertOAuthApplication(r.Context(), s.Pool, name, redirectURIs, website, scopes)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not create app")
		return
	}
	redirectFirst := strings.TrimSpace(redirectURIs)
	if parts := strings.Fields(strings.ReplaceAll(redirectFirst, "\n", " ")); len(parts) > 0 {
		redirectFirst = parts[0]
	}
	out := map[string]any{
		"id":            strconv.FormatInt(app.ID, 10),
		"name":          app.ClientName,
		"website":       app.Website,
		"redirect_uri":  redirectFirst,
		"redirect_uris": strings.TrimSpace(redirectURIs),
		"client_id":     app.ClientID,
		"client_secret": app.ClientSecret,
		"vapid_key":     "",
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) accountMap(ctx context.Context, actorID int64) (map[string]any, error) {
	uname, dom, id, profile, created, err := store.ActorForMastodon(ctx, s.Pool, actorID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":              strconv.FormatInt(id, 10),
		"username":        uname,
		"acct":            fmt.Sprintf("%s@%s", uname, dom),
		"display_name":    uname,
		"locked":          false,
		"bot":             false,
		"discoverable":    false,
		"group":           false,
		"noindex":         false,
		"created_at":      created.UTC().Format(time.RFC3339),
		"note":            "",
		"url":             profile,
		"avatar":          "",
		"avatar_static":   "",
		"header":          "",
		"header_static":   "",
		"followers_count": 0,
		"following_count": 0,
		"statuses_count":  0,
		"last_status_at":  nil,
		"emojis":          []any{},
		"fields":          []any{},
		"roles":           []any{},
	}, nil
}

func (s *Server) getVerifyCredentials(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	m, err := s.accountMap(r.Context(), actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(m)
}

type statusCreateJSON struct {
	Status           string  `json:"status"`
	Visibility       string  `json:"visibility"`
	QuotedStatusID   *int64  `json:"quoted_status_id,omitempty"`
	InReplyToID      *int64  `json:"in_reply_to_id,omitempty"`
	MediaIDs         []int64 `json:"media_ids,omitempty"`
	DirectAccountIDs []int64 `json:"direct_account_ids,omitempty"`
	Sensitive        bool    `json:"sensitive"`
	SpoilerText      string  `json:"spoiler_text"`
}

func (s *Server) postStatuses(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	uname, _, _, _, _, err := store.ActorForMastodon(r.Context(), s.Pool, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	if !s.H.IsLocalActor(uname) {
		writeAPIError(w, http.StatusForbidden, "not a local account")
		return
	}
	ct := r.Header.Get("Content-Type")
	var text string
	var quotedStatusID *int64
	var inReplyToID *int64
	var mediaIDs []int64
	var directAccountIDs []int64
	statusSensitive := false
	spoilerText := ""
	visibility := "public"
	if strings.Contains(strings.ToLower(ct), "application/json") {
		var body statusCreateJSON
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid json")
			return
		}
		text = body.Status
		quotedStatusID = body.QuotedStatusID
		inReplyToID = body.InReplyToID
		mediaIDs = body.MediaIDs
		directAccountIDs = body.DirectAccountIDs
		statusSensitive = body.Sensitive
		spoilerText = strings.TrimSpace(body.SpoilerText)
		if strings.TrimSpace(body.Visibility) != "" {
			visibility = strings.TrimSpace(strings.ToLower(body.Visibility))
		}
	} else {
		if err := r.ParseForm(); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid form")
			return
		}
		text = r.FormValue("status")
		if q := strings.TrimSpace(r.FormValue("quoted_status_id")); q != "" {
			n, err := strconv.ParseInt(q, 10, 64)
			if err == nil && n > 0 {
				quotedStatusID = &n
			}
		}
		if q := strings.TrimSpace(r.FormValue("in_reply_to_id")); q != "" {
			n, err := strconv.ParseInt(q, 10, 64)
			if err == nil && n > 0 {
				inReplyToID = &n
			}
		}
		if v := strings.TrimSpace(strings.ToLower(r.FormValue("visibility"))); v != "" {
			visibility = v
		}
		statusSensitive = parseBoolish(r.FormValue("sensitive"))
		spoilerText = strings.TrimSpace(r.FormValue("spoiler_text"))
	}
	text = strings.TrimSpace(text)
	spoilerText = strings.TrimSpace(spoilerText)
	if text == "" && len(mediaIDs) == 0 && spoilerText == "" {
		writeAPIError(w, http.StatusBadRequest, "Validation failed: Text can't be blank")
		return
	}
	maxMedia := s.cfg().EffectiveMediaMaxAttachmentsPerStatus()
	if len(mediaIDs) > maxMedia {
		writeAPIError(w, http.StatusUnprocessableEntity, "Validation failed: Too many media attachments")
		return
	}
	if visibility != "public" && visibility != "unlisted" && visibility != "private" && visibility != "direct" {
		writeAPIError(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	root := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	actID := newIRI(root, "activities")
	noteID := newIRI(root, "objects")
	prof := s.cfg().LocalActorProfileURL(uname)
	followers := s.cfg().LocalActorFollowersURL(uname)
	pub := "https://www.w3.org/ns/activitystreams#Public"
	var to []string
	var cc []string
	switch visibility {
	case "public":
		to = []string{pub}
		cc = []string{followers}
	case "unlisted":
		to = []string{followers}
		cc = []string{pub}
	case "private":
		to = []string{followers}
		cc = []string{}
	case "direct":
		to = []string{}
		cc = []string{}
		for _, aid := range directAccountIDs {
			if aid < 1 {
				continue
			}
			aurl, _, _, err := store.ActorProfileByID(r.Context(), s.Pool, aid)
			if err != nil || strings.TrimSpace(aurl) == "" {
				writeAPIError(w, http.StatusUnprocessableEntity, "invalid direct recipient")
				return
			}
			to = append(to, strings.TrimRight(strings.TrimSpace(aurl), "/"))
		}
		if len(to) == 0 {
			writeAPIError(w, http.StatusUnprocessableEntity, "direct visibility requires direct_account_ids")
			return
		}
	}
	contentHTML := "<p></p>"
	if text != "" {
		contentHTML = "<p>" + htmlEscapeBasic(text) + "</p>"
	}
	note := map[string]any{
		"type":         "Note",
		"id":           noteID,
		"attributedTo": prof,
		"content":      contentHTML,
		"to":           to,
		"cc":           cc,
	}
	if spoilerText != "" {
		note["summary"] = spoilerText
	}
	note[noteVisibilityKey] = visibility
	if visibility == "direct" {
		ids := make([]any, 0, len(directAccountIDs))
		for _, id := range directAccountIDs {
			ids = append(ids, float64(id))
		}
		note[noteDirectRecipientsKey] = ids
	}
	mediaSensitive := false
	if len(mediaIDs) > 0 {
		atts := make([]any, 0, len(mediaIDs))
		for _, mid := range mediaIDs {
			if mid < 1 {
				continue
			}
			mr, err := store.GetMastodonMediaForActor(r.Context(), s.Pool, mid, actorID)
			if err != nil {
				writeAPIError(w, http.StatusUnprocessableEntity, "invalid media_ids")
				return
			}
			if mr.ProcessingState != store.MediaProcessingComplete {
				writeAPIError(w, http.StatusUnprocessableEntity, "media is still processing")
				return
			}
			if mr.Sensitive {
				mediaSensitive = true
			}
			mediaURL := root + "/media/" + mr.BlobKey
			doc := map[string]any{
				"type":      "Document",
				"mediaType": mr.ContentType,
				"url":       mediaURL,
			}
			if mr.Description != "" {
				doc["name"] = mr.Description
			}
			if mr.Sensitive {
				doc["sensitive"] = true
			}
			atts = append(atts, doc)
		}
		if len(atts) > 0 {
			note["attachment"] = atts
		}
	}
	combinedSensitive := statusSensitive || mediaSensitive
	note[noteSensitiveKey] = combinedSensitive
	if combinedSensitive {
		note["sensitive"] = true
	}
	note[noteSpoilerTextKey] = spoilerText
	if quotedStatusID != nil && *quotedStatusID > 0 {
		qrow, err := store.GetActivityByID(r.Context(), s.Pool, *quotedStatusID)
		if err != nil || qrow == nil || !strings.EqualFold(strings.TrimSpace(qrow.Type), "create") {
			writeAPIError(w, http.StatusNotFound, "Record not found")
			return
		}
		qres, ok := resolveCreateStatusRow(qrow)
		if !ok {
			writeAPIError(w, http.StatusNotFound, "Record not found")
			return
		}
		note[quotedStatusActivityKey] = float64(*quotedStatusID)
		note["quoteUri"] = qres.NoteIRI
		extra := "<p><span class=\"quote-inline\"><a href=\"" + htmlEscapeBasic(qres.NoteIRI) + "\">[" + strconv.FormatInt(*quotedStatusID, 10) + "]</a></span></p>"
		note["content"] = note["content"].(string) + extra
	}
	if inReplyToID != nil && *inReplyToID > 0 {
		prow, err := store.GetActivityByID(r.Context(), s.Pool, *inReplyToID)
		if err != nil || prow == nil || !strings.EqualFold(strings.TrimSpace(prow.Type), "create") {
			writeAPIError(w, http.StatusNotFound, "Record not found")
			return
		}
		pres, ok := resolveCreateStatusRow(prow)
		if !ok {
			writeAPIError(w, http.StatusNotFound, "Record not found")
			return
		}
		note["inReplyTo"] = pres.NoteIRI
		note[internalInReplyToActivityKey] = float64(*inReplyToID)
	}
	create := map[string]any{
		"@context": []any{"https://www.w3.org/ns/activitystreams"},
		"type":     "Create",
		"id":       actID,
		"actor":    prof,
		"to":       to,
		"cc":       cc,
		"object":   note,
	}
	raw, err := json.Marshal(create)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not build activity")
		return
	}
	if err := s.H.PublishLocalActivityBytes(r.Context(), uname, raw); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	statusDBID, err := store.ActivityIDByActorAndActivityIRI(r.Context(), s.Pool, actorID, actID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load posted status")
		return
	}
	row, err := store.GetActivityByID(r.Context(), s.Pool, statusDBID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load posted status")
		return
	}
	out, ok := s.mastodonStatusPresentation(r.Context(), *row, actorID)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "could not build status")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func htmlEscapeBasic(s string) string {
	r := strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `>`, "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func newIRI(root, kind string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s/#/%s/%s", strings.TrimRight(root, "/"), kind, hex.EncodeToString(b[:]))
}

func (s *Server) getTimelineHome(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	ctx := r.Context()
	rows, err := store.ListRecentCreateActivitiesForActor(ctx, s.Pool, actorID, timelineLimit(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load timeline")
		return
	}
	rows = s.filterActivityRowsForViewer(ctx, actorID, rows, "home")
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		st, ok := s.mastodonStatusPresentation(ctx, row, actorID)
		if ok {
			out = append(out, st)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// normalizeMastodonSearchQuery trims the query and strips a single leading "@"
// so "@user@remote" and "user@remote" both resolve (Mastodon clients often send the former).
func normalizeMastodonSearchQuery(q string) string {
	q = strings.TrimSpace(q)
	if strings.HasPrefix(q, "@") {
		q = strings.TrimSpace(strings.TrimPrefix(q, "@"))
	}
	return strings.TrimSpace(q)
}

func (s *Server) getAccountSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := normalizeMastodonSearchQuery(r.URL.Query().Get("q"))
	if q == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode([]any{})
		return
	}
	ctx := r.Context()
	host := s.instanceHost()
	cli := s.H.FederationHTTPClient()

	// Bare local handle (Ivory/Mastodon often search without repeating @host for the current instance).
	if s.Pool != nil && !strings.Contains(q, "@") &&
		!strings.HasPrefix(strings.ToLower(q), "acct:") &&
		!strings.HasPrefix(q, "http://") && !strings.HasPrefix(q, "https://") {
		if id, err := store.LocalActorIDForInstanceUsernameCI(ctx, s.Pool, host, q); err == nil {
			if m, err := s.accountMap(ctx, id); err == nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_ = json.NewEncoder(w).Encode([]any{m})
				return
			}
		}
	}

	// Local acct:user@thishost
	if user, dom, ok := parseAcct(q); ok && strings.EqualFold(dom, host) && s.H.IsLocalActor(user) {
		id, ok := s.H.LocalActorID(user)
		if ok {
			if m, err := s.accountMap(ctx, id); err == nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_ = json.NewEncoder(w).Encode([]any{m})
				return
			}
		}
	}
	if user, dom, ok := parseAcct(q); ok && dom != "" {
		prof, err := webfingerProfileURL(ctx, cli, user, dom)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		m, err := s.accountFromActorURL(ctx, prof)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode([]any{m})
		return
	}
	if strings.HasPrefix(q, "http://") || strings.HasPrefix(q, "https://") {
		m, err := s.accountFromActorURL(ctx, q)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode([]any{m})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode([]any{})
}

func parseAcct(q string) (user, host string, ok bool) {
	q = strings.TrimSpace(q)
	if strings.HasPrefix(strings.ToLower(q), "acct:") {
		q = q[5:]
	}
	user, host, ok = strings.Cut(q, "@")
	user, host = strings.TrimSpace(user), strings.TrimSpace(host)
	return user, host, ok && user != "" && host != ""
}

func webfingerProfileURL(ctx context.Context, cli *http.Client, user, host string) (string, error) {
	resource := url.QueryEscape(fmt.Sprintf("acct:%s@%s", user, host))
	wfu := fmt.Sprintf("https://%s/.well-known/webfinger?resource=%s", host, resource)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wfu, nil)
	if err != nil {
		return "", err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("webfinger: %s", resp.Status)
	}
	var doc struct {
		Links []struct {
			Rel  string `json:"rel"`
			Type string `json:"type"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", err
	}
	links := make([]webfingerLink, 0, len(doc.Links))
	for _, l := range doc.Links {
		links = append(links, webfingerLink{Rel: l.Rel, Type: l.Type, Href: l.Href})
	}
	return pickActorHrefFromWebfingerLinks(links)
}

type webfingerLink struct {
	Rel  string
	Type string
	Href string
}

// pickActorHrefFromWebFingerLinks selects the actor document href from WebFinger JRD links.
// Mastodon often advertises application/activity+json; many stacks use application/ld+json with an ActivityStreams profile instead.
func pickActorHrefFromWebfingerLinks(links []webfingerLink) (string, error) {
	var ldFallback string
	for _, l := range links {
		if !strings.EqualFold(strings.TrimSpace(l.Rel), "self") {
			continue
		}
		href := strings.TrimSpace(l.Href)
		if href == "" {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(l.Type))
		if strings.Contains(t, "activity+json") {
			return href, nil
		}
		if strings.Contains(t, "ld+json") {
			if ldFallback == "" {
				ldFallback = href
			}
		}
	}
	if ldFallback != "" {
		return ldFallback, nil
	}
	return "", fmt.Errorf("no suitable ActivityPub self link in WebFinger")
}

func (s *Server) accountFromActorURL(ctx context.Context, actorURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, actorURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/activity+json, application/ld+json")
	resp, err := s.H.FederationHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("actor: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	preferred, _ := doc["preferredUsername"].(string)
	u, _ := url.Parse(store.CanonicalActorURL(actorURL))
	dom := ""
	if u != nil {
		dom = u.Hostname()
	}
	acct := preferred + "@" + dom
	idRemote := int64(0)
	if store.CanonicalActorURL(actorURL) != "" {
		idRemote, err = store.EnsureRemoteActor(ctx, s.Pool, actorURL, "(search)")
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"id":              strconv.FormatInt(idRemote, 10),
		"username":        preferred,
		"acct":            acct,
		"display_name":    stringField(doc, "name"),
		"locked":          false,
		"bot":             false,
		"discoverable":    false,
		"group":           false,
		"noindex":         false,
		"created_at":      time.Now().UTC().Format(time.RFC3339),
		"note":            stringField(doc, "summary"),
		"url":             actorURL,
		"avatar":          "",
		"avatar_static":   "",
		"header":          "",
		"header_static":   "",
		"followers_count": 0,
		"following_count": 0,
		"statuses_count":  0,
		"last_status_at":  nil,
		"emojis":          []any{},
		"fields":          []any{},
		"roles":           []any{},
	}, nil
}

func stringField(doc map[string]any, k string) string {
	if v, ok := doc[k].(string); ok {
		return v
	}
	return ""
}

func (s *Server) postAccountFollow(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rawID := r.PathValue("id")
	targetID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || targetID < 1 {
		writeAPIError(w, http.StatusBadRequest, "invalid id")
		return
	}
	targetURL, _, _, err := store.ActorProfileByID(r.Context(), s.Pool, targetID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	uname, _, _, _, _, err := store.ActorForMastodon(r.Context(), s.Pool, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	if !s.H.IsLocalActor(uname) {
		writeAPIError(w, http.StatusForbidden, "not a local account")
		return
	}
	prof := s.cfg().LocalActorProfileURL(uname)
	root := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	followActivityID := newIRI(root, "activities")
	followMap := map[string]any{
		"@context": []any{"https://www.w3.org/ns/activitystreams"},
		"type":     "Follow",
		"id":       followActivityID,
		"actor":    prof,
		"to":       []any{targetURL},
		"object":   targetURL,
	}
	rawFollow, err := json.Marshal(followMap)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not build activity")
		return
	}
	if err := s.H.PublishLocalActivityBytes(r.Context(), uname, rawFollow); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := map[string]any{
		"id":                   strconv.FormatInt(targetID, 10),
		"following":            false,
		"requested":            true,
		"showing_reblogs":      true,
		"notifying":            false,
		"followed_by":          false,
		"blocking":             false,
		"blocked_by":           false,
		"muting":               false,
		"muting_notifications": false,
		"endorsed":             false,
		"note":                 "",
		"account":              nil,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) postAccountUnfollow(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rawID := r.PathValue("id")
	targetID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || targetID < 1 {
		writeAPIError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx := r.Context()
	uname, _, _, _, _, err := store.ActorForMastodon(ctx, s.Pool, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	if !s.H.IsLocalActor(uname) {
		writeAPIError(w, http.StatusForbidden, "not a local account")
		return
	}
	followAct, followeeURL, ok, err := store.LookupActiveFollowForUndo(ctx, s.Pool, actorID, targetID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load relationship")
		return
	}
	if !ok {
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"id":                   rawID,
			"following":            false,
			"requested":            false,
			"showing_reblogs":      true,
			"notifying":            false,
			"followed_by":          false,
			"blocking":             false,
			"blocked_by":           false,
			"muting":               false,
			"muting_notifications": false,
			"endorsed":             false,
			"note":                 "",
			"account":              nil,
		})
		return
	}
	prof := s.cfg().LocalActorProfileURL(uname)
	root := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	undoID := newIRI(root, "activities")
	embedFollow := map[string]any{
		"type":   "Follow",
		"id":     followAct,
		"actor":  prof,
		"object": followeeURL,
	}
	undoMap := map[string]any{
		"@context": []any{"https://www.w3.org/ns/activitystreams"},
		"type":     "Undo",
		"id":       undoID,
		"actor":    prof,
		"to":       []any{followeeURL},
		"object":   embedFollow,
	}
	rawUndo, err := json.Marshal(undoMap)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not build activity")
		return
	}
	if err := s.H.PublishLocalActivityBytes(ctx, uname, rawUndo); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONResponse(w, http.StatusOK, map[string]any{
		"id":                   rawID,
		"following":            false,
		"requested":            false,
		"showing_reblogs":      true,
		"notifying":            false,
		"followed_by":          false,
		"blocking":             false,
		"blocked_by":           false,
		"muting":               false,
		"muting_notifications": false,
		"endorsed":             false,
		"note":                 "",
		"account":              nil,
	})
}
