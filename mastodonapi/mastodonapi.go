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
	Status     string `json:"status"`
	Visibility string `json:"visibility"`
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
	if strings.Contains(strings.ToLower(ct), "application/json") {
		var body statusCreateJSON
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid json")
			return
		}
		text = body.Status
	} else {
		if err := r.ParseForm(); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid form")
			return
		}
		text = r.FormValue("status")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		writeAPIError(w, http.StatusBadRequest, "Validation failed: Text can't be blank")
		return
	}
	root := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	actID := newIRI(root, "activities")
	noteID := newIRI(root, "objects")
	prof := s.cfg().LocalActorProfileURL(uname)
	followers := s.cfg().LocalActorFollowersURL(uname)
	note := map[string]any{
		"type":         "Note",
		"id":           noteID,
		"attributedTo": prof,
		"content":      "<p>" + htmlEscapeBasic(text) + "</p>",
		"to":           []string{"https://www.w3.org/ns/activitystreams#Public"},
		"cc":           []string{followers},
	}
	create := map[string]any{
		"@context": []any{"https://www.w3.org/ns/activitystreams"},
		"type":     "Create",
		"id":       actID,
		"actor":    prof,
		"to":       []string{"https://www.w3.org/ns/activitystreams#Public"},
		"cc":       []string{followers},
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
	// Minimal Status entity (enough for Ivory to treat as success).
	out := map[string]any{
		"id":                strings.TrimPrefix(noteID, root+"/"),
		"uri":               noteID,
		"created_at":        time.Now().UTC().Format(time.RFC3339),
		"content":           note["content"],
		"visibility":        "public",
		"language":          "en",
		"url":               noteID,
		"replies_count":     0,
		"reblogs_count":     0,
		"favourites_count":  0,
		"favourited":        false,
		"reblogged":         false,
		"sensitive":         false,
		"spoiler_text":      "",
		"muted":             false,
		"pinned":            false,
		"bookmarked":        false,
		"account":           nil,
		"media_attachments": []any{},
		"mentions":          []any{},
		"tags":              []any{},
		"emojis":            []any{},
		"card":              nil,
		"poll":              nil,
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
	_ = actorID
	out := []any{}
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
