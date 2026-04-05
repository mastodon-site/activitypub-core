package mastodonapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/inboxproc"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

type socialExtNoopQueue struct{}

func (socialExtNoopQueue) Enqueue(ctx context.Context, job queue.Job) error { return nil }

func (socialExtNoopQueue) Dequeue(ctx context.Context) (*queue.Lease, error) { return nil, nil }

func (socialExtNoopQueue) Ack(ctx context.Context, id int64) error { return nil }

func (socialExtNoopQueue) Nack(ctx context.Context, id int64, requeue bool) error { return nil }

func socialExtMustPostStatus(t *testing.T, mux *http.ServeMux, bearer, jsonBody string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /statuses: %d %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func socialExtMustPostEmpty(t *testing.T, mux *http.ServeMux, method, path, bearer string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func socialExtGETArray(t *testing.T, mux *http.ServeMux, path, bearer string) []any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var arr []any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatal(err)
	}
	return arr
}

// TestIntegration_Social_localToLocal exercises favourite / reblog / bookmark between two local accounts
// (Mastodon REST + federation edge tables via outbox).
func TestIntegration_Social_localToLocal(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	st, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	truncateMastodonTestDB(t, st.Pool)

	cfg := &config.Config{
		PublicBaseURL:  "https://social-local.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice", "bob"},
	}
	h, err := aphttp.New(cfg, mastodonIntegrationAPHTTPDeps(cfg, st))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := &Server{H: h, Pool: st.Pool}
	srv.mountMastodon(mux)

	aliceID, _ := h.LocalActorID("alice")
	bobID, _ := h.LocalActorID("bob")
	if aliceID < 1 || bobID < 1 {
		t.Fatalf("actors alice=%d bob=%d", aliceID, bobID)
	}

	app, err := store.InsertOAuthApplication(ctx, st.Pool, "soc", "https://app/cb", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	const tokAlice = "social-local-alice"
	const tokBob = "social-local-bob"
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(tokAlice), app.ID, aliceID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(tokBob), app.ID, bobID); err != nil {
		t.Fatal(err)
	}

	bobPost := socialExtMustPostStatus(t, mux, tokBob, `{"status":"bob origin\"s post"}`)
	bobStatusID := fmt.Sprint(bobPost["id"])
	if bobStatusID == "" || bobStatusID == "0" {
		t.Fatal(bobPost)
	}

	// Favourite
	fav := socialExtMustPostEmpty(t, mux, http.MethodPost, "/api/v1/statuses/"+bobStatusID+"/favourite", tokAlice)
	if fav["favourited"] != true {
		t.Fatalf("favourited: %#v", fav["favourited"])
	}
	if n, _ := fav["favourites_count"].(float64); n < 1 {
		t.Fatalf("favourites_count: %#v", fav["favourites_count"])
	}

	by := socialExtGETArray(t, mux, "/api/v1/statuses/"+bobStatusID+"/favourited_by", "")
	if len(by) != 1 {
		t.Fatalf("favourited_by len %d", len(by))
	}
	acct, _ := by[0].(map[string]any)
	if acct["username"] != "alice" {
		t.Fatalf("liker username: %#v", acct["username"])
	}

	favTL := socialExtGETArray(t, mux, "/api/v1/favourites", tokAlice)
	if len(favTL) != 1 || fmt.Sprint(asMap(favTL[0])["id"]) != bobStatusID {
		t.Fatalf("favourites timeline: %#v", favTL)
	}

	// Unfavourite
	unf := socialExtMustPostEmpty(t, mux, http.MethodPost, "/api/v1/statuses/"+bobStatusID+"/unfavourite", tokAlice)
	if unf["favourited"] != false {
		t.Fatalf("after unfav: %#v", unf["favourited"])
	}
	if int(unf["favourites_count"].(float64)) != 0 {
		t.Fatalf("favourites_count after unfav: %#v", unf["favourites_count"])
	}

	// Reblog
	re := socialExtMustPostEmpty(t, mux, http.MethodPost, "/api/v1/statuses/"+bobStatusID+"/reblog", tokAlice)
	if re["reblogged"] != true {
		t.Fatalf("reblogged: %#v", re["reblogged"])
	}
	if n, _ := re["reblogs_count"].(float64); n < 1 {
		t.Fatalf("reblogs_count: %#v", re["reblogs_count"])
	}
	rbBy := socialExtGETArray(t, mux, "/api/v1/statuses/"+bobStatusID+"/reblogged_by", "")
	if len(rbBy) != 1 {
		t.Fatalf("reblogged_by len %d", len(rbBy))
	}
	if asMap(rbBy[0])["username"] != "alice" {
		t.Fatalf("reblogger: %#v", rbBy[0])
	}

	unre := socialExtMustPostEmpty(t, mux, http.MethodPost, "/api/v1/statuses/"+bobStatusID+"/unreblog", tokAlice)
	if unre["reblogged"] != false {
		t.Fatalf("after unreblog: %#v", unre["reblogged"])
	}

	// Bookmark (local-only)
	bm := socialExtMustPostEmpty(t, mux, http.MethodPost, "/api/v1/statuses/"+bobStatusID+"/bookmark", tokAlice)
	if bm["bookmarked"] != true {
		t.Fatalf("bookmarked: %#v", bm["bookmarked"])
	}
	bmTL := socialExtGETArray(t, mux, "/api/v1/bookmarks", tokAlice)
	if len(bmTL) != 1 || fmt.Sprint(asMap(bmTL[0])["id"]) != bobStatusID {
		t.Fatalf("bookmarks list: %#v", bmTL)
	}
	unbm := socialExtMustPostEmpty(t, mux, http.MethodPost, "/api/v1/statuses/"+bobStatusID+"/unbookmark", tokAlice)
	if unbm["bookmarked"] != false {
		t.Fatalf("after unbookmark: %#v", unbm["bookmarked"])
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// TestIntegration_Social_quotePost checks quoted_status_id → quoted_status in API JSON.
func TestIntegration_Social_quotePost(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	st, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	truncateMastodonTestDB(t, st.Pool)

	cfg := &config.Config{
		PublicBaseURL:  "https://quote-ext.test",
		LocalUsername:  "bob",
		LocalUsernames: []string{"alice", "bob"},
	}
	h, err := aphttp.New(cfg, mastodonIntegrationAPHTTPDeps(cfg, st))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Server{H: h, Pool: st.Pool}).mountMastodon(mux)

	aliceID, _ := h.LocalActorID("alice")
	app, err := store.InsertOAuthApplication(ctx, st.Pool, "q", "https://app/cb", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	const tokAlice = "quote-ext-alice"
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(tokAlice), app.ID, aliceID); err != nil {
		t.Fatal(err)
	}

	orig := socialExtMustPostStatus(t, mux, tokAlice, `{"status":"original for quote"}`)
	origID := fmt.Sprint(orig["id"])
	body := fmt.Sprintf(`{"status":"my take","quoted_status_id":%s}`, origID)
	qpost := socialExtMustPostStatus(t, mux, tokAlice, body)
	qs, ok := qpost["quoted_status"].(map[string]any)
	if !ok || fmt.Sprint(qs["id"]) != origID {
		t.Fatalf("quoted_status: %#v", qpost["quoted_status"])
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+fmt.Sprint(qpost["id"]), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get quote status: %d %s", rec.Code, rec.Body.String())
	}
	var fetched map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	qs2, ok := fetched["quoted_status"].(map[string]any)
	if !ok || fmt.Sprint(qs2["id"]) != origID {
		t.Fatalf("GET quoted_status: %#v", fetched["quoted_status"])
	}
}

// TestIntegration_Social_localFavouritesRemoteNote: inbox Create (remote) → local user favourites via Mastodon API (Like to author inbox).
func TestIntegration_Social_localFavouritesRemoteNote(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	st, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	truncateMastodonTestDB(t, st.Pool)

	cfg := &config.Config{
		PublicBaseURL:  "https://fav-remote-note.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
	}
	h, err := aphttp.New(cfg, mastodonIntegrationAPHTTPDeps(cfg, st))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Server{H: h, Pool: st.Pool}).mountMastodon(mux)

	aliceID, ok := h.LocalActorID("alice")
	if !ok || aliceID < 1 {
		t.Fatalf("alice id=%d", aliceID)
	}
	remoteID, err := store.EnsureRemoteActor(ctx, st.Pool, "https://fed.remote/users/sam", "pem")
	if err != nil {
		t.Fatal(err)
	}

	note := map[string]any{
		"id":           "https://fed.remote/obj/note-x",
		"type":         "Note",
		"attributedTo": "https://fed.remote/users/sam",
		"content":      "remote hello",
	}
	noteRaw, err := json.Marshal(note)
	if err != nil {
		t.Fatal(err)
	}
	actCreate := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       "https://fed.remote/act/cr-x",
		"type":     "Create",
		"actor":    "https://fed.remote/users/sam",
		"to":       []string{cfg.LocalActorProfileURL("alice")},
		"object":   json.RawMessage(noteRaw),
	}
	createRaw, err := json.Marshal(actCreate)
	if err != nil {
		t.Fatal(err)
	}
	ins, createDBID, err := store.InsertInboundActivity(ctx, st.Pool, remoteID, "https://fed.remote/act/cr-x", "Create", createRaw)
	if err != nil || !ins {
		t.Fatalf("insert create: %v ins=%v", err, ins)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, st.Pool, socialExtNoopQueue{}, cfg, nil, createDBID, nil); err != nil {
		t.Fatal(err)
	}
	remoteStatusID := strconv.FormatInt(createDBID, 10)

	app, err := store.InsertOAuthApplication(ctx, st.Pool, "frn", "https://app/cb", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	const tokAlice = "fav-remote-note-alice"
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(tokAlice), app.ID, aliceID); err != nil {
		t.Fatal(err)
	}

	fav := socialExtMustPostEmpty(t, mux, http.MethodPost, "/api/v1/statuses/"+remoteStatusID+"/favourite", tokAlice)
	if fav["favourited"] != true {
		t.Fatalf("favourited: %#v", fav)
	}
	if n, _ := fav["favourites_count"].(float64); n < 1 {
		t.Fatalf("favourites_count: %#v", fav["favourites_count"])
	}

	by := socialExtGETArray(t, mux, "/api/v1/statuses/"+remoteStatusID+"/favourited_by", "")
	if len(by) != 1 || asMap(by[0])["username"] != "alice" {
		t.Fatalf("favourited_by: %#v", by)
	}
	favTL := socialExtGETArray(t, mux, "/api/v1/favourites", tokAlice)
	if len(favTL) != 1 || fmt.Sprint(asMap(favTL[0])["id"]) != remoteStatusID {
		t.Fatalf("favourites timeline: %#v", favTL)
	}
}

// TestIntegration_Social_remoteLikeAndAnnounceLocalPost: federated Like / Announce on a local Create-backed note (inbox side effects + API counts).
func TestIntegration_Social_remoteLikeAndAnnounceLocalPost(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	st, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	truncateMastodonTestDB(t, st.Pool)

	cfg := &config.Config{
		PublicBaseURL:  "https://remote-soc-local.test",
		LocalUsername:  "bob",
		LocalUsernames: []string{"bob"},
	}
	h, err := aphttp.New(cfg, mastodonIntegrationAPHTTPDeps(cfg, st))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Server{H: h, Pool: st.Pool}).mountMastodon(mux)

	bobID, ok := h.LocalActorID("bob")
	if !ok || bobID < 1 {
		t.Fatal(bobID)
	}
	app, err := store.InsertOAuthApplication(ctx, st.Pool, "rl", "https://app/cb", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	const tokBob = "remote-soc-bob"
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(tokBob), app.ID, bobID); err != nil {
		t.Fatal(err)
	}

	bobPost := socialExtMustPostStatus(t, mux, tokBob, `{"status":"local target for fed reactions"}`)
	noteIRI, _ := bobPost["uri"].(string)
	if noteIRI == "" {
		t.Fatalf("status uri: %#v", bobPost["uri"])
	}
	bobStatusID := fmt.Sprint(bobPost["id"])

	remoteID, err := store.EnsureRemoteActor(ctx, st.Pool, "https://fed-in.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}

	likeAct := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       "https://fed-in.test/act/like-local-1",
		"type":     "Like",
		"actor":    "https://fed-in.test/users/ally",
		"to":       []string{cfg.LocalActorProfileURL("bob")},
		"object":   noteIRI,
	}
	likeRaw, err := json.Marshal(likeAct)
	if err != nil {
		t.Fatal(err)
	}
	ins, likeDBID, err := store.InsertInboundActivity(ctx, st.Pool, remoteID, "https://fed-in.test/act/like-local-1", "Like", likeRaw)
	if err != nil || !ins {
		t.Fatalf("insert like: %v", err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, st.Pool, socialExtNoopQueue{}, cfg, nil, likeDBID, nil); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+bobStatusID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status after remote like: %d %s", rec.Code, rec.Body.String())
	}
	var stDoc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &stDoc); err != nil {
		t.Fatal(err)
	}
	if n, _ := stDoc["favourites_count"].(float64); n != 1 {
		t.Fatalf("favourites_count: %#v", stDoc["favourites_count"])
	}
	by := socialExtGETArray(t, mux, "/api/v1/statuses/"+bobStatusID+"/favourited_by", "")
	if len(by) != 1 {
		t.Fatalf("favourited_by: %#v", by)
	}
	if asMap(by[0])["username"] != "ally" || asMap(by[0])["acct"] != "ally@fed-in.test" {
		t.Fatalf("remote liker account: %#v", by[0])
	}

	annAct := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       "https://fed-in.test/act/ann-local-1",
		"type":     "Announce",
		"actor":    "https://fed-in.test/users/ally",
		"to":       []string{"https://www.w3.org/ns/activitystreams#Public"},
		"object":   noteIRI,
	}
	annRaw, err := json.Marshal(annAct)
	if err != nil {
		t.Fatal(err)
	}
	ins2, annDBID, err := store.InsertInboundActivity(ctx, st.Pool, remoteID, "https://fed-in.test/act/ann-local-1", "Announce", annRaw)
	if err != nil || !ins2 {
		t.Fatalf("insert announce: %v", err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, st.Pool, socialExtNoopQueue{}, cfg, nil, annDBID, nil); err != nil {
		t.Fatal(err)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+bobStatusID, nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("get status after announce: %d %s", rec2.Code, rec2.Body.String())
	}
	var stDoc2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &stDoc2); err != nil {
		t.Fatal(err)
	}
	if n, _ := stDoc2["reblogs_count"].(float64); n != 1 {
		t.Fatalf("reblogs_count: %#v", stDoc2["reblogs_count"])
	}
	rb := socialExtGETArray(t, mux, "/api/v1/statuses/"+bobStatusID+"/reblogged_by", "")
	if len(rb) != 1 || asMap(rb[0])["username"] != "ally" {
		t.Fatalf("reblogged_by: %#v", rb)
	}
}
