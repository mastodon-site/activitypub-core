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
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

// TestIntegration_MastodonDBBackedSocialAndTimelines covers handlers that replaced
// empty-array stubs: account statuses / followers / following, public & home timelines,
// GET status & context, plus POST status validation and read paths.
func TestIntegration_MastodonDBBackedSocialAndTimelines(t *testing.T) {
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
		PublicBaseURL:  "https://social-api.test",
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

	aliceID, ok := h.LocalActorID("alice")
	if !ok || aliceID < 1 {
		t.Fatalf("alice id=%d ok=%v", aliceID, ok)
	}
	bobID, ok := h.LocalActorID("bob")
	if !ok || bobID < 1 {
		t.Fatalf("bob id=%d ok=%v", bobID, ok)
	}

	app, err := store.InsertOAuthApplication(ctx, st.Pool, "soc-read", "https://cb/app", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	const rawAlice = "social-test-alice-token"
	const rawBob = "social-test-bob-token"
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(rawAlice), app.ID, aliceID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(rawBob), app.ID, bobID); err != nil {
		t.Fatal(err)
	}

	mustJSONArray := func(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
		t.Helper()
		var out []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("json array: %v body=%s", err, rec.Body.String())
		}
		return out
	}

	postStatus := func(t *testing.T, rawTok, text string) string {
		t.Helper()
		body := `{"status":` + jsonString(text) + `}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+rawTok)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST status %d: %s", rec.Code, rec.Body.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprint(doc["id"])
		if id == "" || id == "0" {
			t.Fatalf("bad id in %#v", doc)
		}
		return id
	}

	t.Run("account_statuses_followers_following_start_empty", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/accounts/" + strconv.FormatInt(aliceID, 10) + "/statuses",
			"/api/v1/accounts/" + strconv.FormatInt(aliceID, 10) + "/followers",
			"/api/v1/accounts/" + strconv.FormatInt(aliceID, 10) + "/following",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
			}
			arr := mustJSONArray(t, rec)
			if len(arr) != 0 {
				t.Fatalf("%s: want empty, got %d elems", path, len(arr))
			}
		}
	})

	t.Run("invalid_account_ids_return_404", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/accounts/0/statuses",
			"/api/v1/accounts/0/followers",
			"/api/v1/accounts/0/following",
			"/api/v1/accounts/-1/statuses",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: want 404, got %d %s", path, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("method_not_allowed", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/timelines/public",
			"/api/v1/accounts/" + strconv.FormatInt(aliceID, 10) + "/followers",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("POST %s: want 405, got %d", path, rec.Code)
			}
		}
	})

	t.Run("get_status_and_context_not_found", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/statuses/0",
			"/api/v1/statuses/999999999",
			"/api/v1/statuses/999999999/context",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: want 404, got %d %s", path, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("post_status_rejects_blank", func(t *testing.T) {
		body := `{"status":"   "}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+rawAlice)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("pending_follow_not_listed_accepted_is", func(t *testing.T) {
		pid := "https://social-api.test/#/follows/pending-1"
		if err := store.UpsertFollow(ctx, st.Pool, bobID, aliceID, pid, store.FollowStatePendingRemote); err != nil {
			t.Fatal(err)
		}
		pathF := "/api/v1/accounts/" + strconv.FormatInt(aliceID, 10) + "/followers"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pathF, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		if n := len(mustJSONArray(t, rec)); n != 0 {
			t.Fatalf("pending follow: want 0 followers, got %d", n)
		}
		if err := store.SetFollowStateByFollowActivityID(ctx, st.Pool, pid, store.FollowStateAccepted); err != nil {
			t.Fatal(err)
		}
		rec2 := httptest.NewRecorder()
		mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, pathF, nil))
		if rec2.Code != http.StatusOK {
			t.Fatal(rec2.Body.String())
		}
		followers := mustJSONArray(t, rec2)
		if len(followers) != 1 {
			t.Fatalf("accepted: want 1 follower, got %d %s", len(followers), rec2.Body.String())
		}
		if fmt.Sprint(followers[0]["username"]) != "bob" {
			t.Fatalf("follower account: %#v", followers[0])
		}

		pathFg := "/api/v1/accounts/" + strconv.FormatInt(bobID, 10) + "/following"
		rec3 := httptest.NewRecorder()
		mux.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, pathFg, nil))
		if rec3.Code != http.StatusOK {
			t.Fatal(rec3.Body.String())
		}
		following := mustJSONArray(t, rec3)
		if len(following) != 1 {
			t.Fatalf("want 1 following, got %d", len(following))
		}
		if fmt.Sprint(following[0]["username"]) != "alice" {
			t.Fatalf("following account: %#v", following[0])
		}
	})

	t.Run("home_timeline_only_own_posts_public_includes_instance", func(t *testing.T) {
		sidAlice := postStatus(t, rawAlice, "only on alice home")
		recBobHome := httptest.NewRecorder()
		mux.ServeHTTP(recBobHome, newAuthedRequest(http.MethodGet, "/api/v1/timelines/home", rawBob))
		if recBobHome.Code != http.StatusOK {
			t.Fatal(recBobHome.Body.String())
		}
		for _, st := range mustJSONArray(t, recBobHome) {
			if fmt.Sprint(st["id"]) == sidAlice {
				t.Fatal("bob home must not contain alice post")
			}
		}

		recPub := httptest.NewRecorder()
		mux.ServeHTTP(recPub, httptest.NewRequest(http.MethodGet, "/api/v1/timelines/public", nil))
		if recPub.Code != http.StatusOK {
			t.Fatal(recPub.Body.String())
		}
		found := false
		for _, st := range mustJSONArray(t, recPub) {
			if fmt.Sprint(st["id"]) == sidAlice {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("public timeline should list local alice post")
		}
	})

	t.Run("get_status_embeds_account_and_context_shape", func(t *testing.T) {
		sid := postStatus(t, rawAlice, "embedded account check")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+sid, nil))
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Body.String())
		}
		var st map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		acct, ok := st["account"].(map[string]any)
		if !ok || acct["username"] != "alice" {
			t.Fatalf("account: %#v", st["account"])
		}
		rec2 := httptest.NewRecorder()
		mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+sid+"/context", nil))
		if rec2.Code != http.StatusOK {
			t.Fatal(rec2.Body.String())
		}
		var ctxDoc map[string]any
		if err := json.Unmarshal(rec2.Body.Bytes(), &ctxDoc); err != nil {
			t.Fatal(err)
		}
		anc, _ := ctxDoc["ancestors"].([]any)
		desc, _ := ctxDoc["descendants"].([]any)
		if anc == nil || desc == nil {
			t.Fatalf("context shape: %#v", ctxDoc)
		}
		if len(anc) != 0 || len(desc) != 0 {
			t.Fatalf("empty ancestors/descendants for flat thread: %#v", ctxDoc)
		}
	})

	t.Run("timeline_limit_applied", func(t *testing.T) {
		mark := "limit-mark-" + strconv.FormatInt(aliceID, 10)
		for i := 0; i < 3; i++ {
			_ = postStatus(t, rawAlice, fmt.Sprintf("%s-%d", mark, i))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newAuthedRequest(http.MethodGet, "/api/v1/timelines/home?limit=2", rawAlice))
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Body.String())
		}
		list := mustJSONArray(t, rec)
		if len(list) != 2 {
			t.Fatalf("limit=2: got %d entries", len(list))
		}
		if !strings.Contains(fmt.Sprint(list[0]["content"]), mark) {
			t.Fatalf("newest first? first=%#v", list[0]["content"])
		}
	})

	t.Run("account_statuses_limit", func(t *testing.T) {
		rec := httptest.NewRecorder()
		path := "/api/v1/accounts/" + strconv.FormatInt(aliceID, 10) + "/statuses?limit=2"
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Body.String())
		}
		if n := len(mustJSONArray(t, rec)); n != 2 {
			t.Fatalf("account statuses limit=2: got %d", n)
		}
	})
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
