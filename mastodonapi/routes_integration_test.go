package mastodonapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("AP_TEST_DATABASE_URL")
	if u == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AP_TEST_DATABASE_URL is required in CI (see GitHub Actions services postgres)")
		}
		t.Skip("set AP_TEST_DATABASE_URL to run integration tests against Postgres (SQL queue + timelines)")
	}
	return u
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "db", "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				t.Fatal(err)
			}
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find db/migrations")
	return ""
}

// mastodonStubRoundTripper stubs HTTPS requests to local profile URLs on publicBaseURL so
// PublishLocalActivityBytes can resolve actor inboxes without DNS (CI hosts like *.test do not resolve).
type mastodonStubRoundTripper func(*http.Request) (*http.Response, error)

func (f mastodonStubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mastodonIntegrationStubFetchClient(publicBaseURL string) *http.Client {
	return mastodonIntegrationStubFetchClientWithRemote(publicBaseURL, "", "")
}

// mastodonIntegrationStubFetchClientWithRemote resolves local profiles on publicBaseURL and optionally
// one remote actor IRI (prefix match) to remoteInboxURL — used when fan-out must target an external inbox.
func mastodonIntegrationStubFetchClientWithRemote(publicBaseURL, remoteActorURL, remoteInboxURL string) *http.Client {
	base := strings.TrimRight(publicBaseURL, "/")
	rAct := strings.TrimRight(strings.TrimSpace(remoteActorURL), "/")
	rInbox := strings.TrimSpace(remoteInboxURL)
	fallback := fetch.NewHTTPClientForPolicy(fetch.TestingPolicy(), 30*time.Second)
	return &http.Client{Transport: mastodonStubRoundTripper(func(req *http.Request) (*http.Response, error) {
		u := req.URL.String()
		uCanon := strings.TrimRight(u, "/")
		if strings.HasPrefix(u, base+"/@") || strings.HasPrefix(u, base+"/users/") {
			inbox := base + "/inbox"
			doc := map[string]any{"id": uCanon, "inbox": inbox}
			b, err := json.Marshal(doc)
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/activity+json"}},
				Body:       io.NopCloser(bytes.NewReader(b)),
				Request:    req,
			}, nil
		}
		if rAct != "" && rInbox != "" && strings.HasPrefix(uCanon, rAct) {
			doc := map[string]any{"id": rAct, "inbox": rInbox}
			b, err := json.Marshal(doc)
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/activity+json"}},
				Body:       io.NopCloser(bytes.NewReader(b)),
				Request:    req,
			}, nil
		}
		return fallback.Transport.RoundTrip(req)
	})}
}

func mastodonIntegrationAPHTTPDeps(cfg *config.Config, st *store.Postgres) aphttp.Deps {
	return aphttp.Deps{
		Store:       st,
		Queue:       sqlqueue.New(st.Pool),
		FetchPolicy: fetch.TestingPolicy(),
		FetchClient: mastodonIntegrationStubFetchClient(cfg.PublicBaseURL),
	}
}

func truncateMastodonTestDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		TRUNCATE TABLE
			oauth_access_tokens,
			oauth_authorization_codes,
			local_accounts,
			oauth_applications,
			queue_jobs,
			deliveries,
			follows,
			status_bookmarks,
			federated_likes,
			federated_announces,
			federated_blocks,
			activities,
			objects,
			actors
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

func hashAccessToken(raw string) []byte {
	s := sha256.Sum256([]byte(raw))
	return s[:]
}

func newAuthedRequest(method, path, rawToken string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if rawToken != "" {
		req.Header.Set("Authorization", "Bearer "+rawToken)
	}
	return req
}

func TestIntegration_MastodonRoutes_withDatabase(t *testing.T) {
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

	cfg := &config.Config{PublicBaseURL: "https://routes-int.test", LocalUsername: "alice", LocalUsernames: []string{"alice"}}
	h, err := aphttp.New(cfg, mastodonIntegrationAPHTTPDeps(cfg, st))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := &Server{H: h, Pool: st.Pool}
	srv.mountMastodon(mux)

	actorID, ok := h.LocalActorID("alice")
	if !ok || actorID < 1 {
		t.Fatalf("local actor alice: id=%d ok=%v", actorID, ok)
	}

	const rawToken = "integration-test-token-unsafe"
	app, err := store.InsertOAuthApplication(ctx, st.Pool, "testapp", "https://app/cb", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(rawToken), app.ID, actorID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("verify_credentials", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newAuthedRequest(http.MethodGet, "/api/v1/accounts/verify_credentials", rawToken))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var acct map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &acct); err != nil {
			t.Fatal(err)
		}
		if acct["username"] != "alice" {
			t.Fatalf("username: %#v", acct["username"])
		}
	})

	t.Run("get_account_by_id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		path := "/api/v1/accounts/" + strconv.FormatInt(actorID, 10)
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var acct map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &acct); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(acct["id"]) != strconv.FormatInt(actorID, 10) {
			t.Fatalf("id field: %#v", acct["id"])
		}
		if acct["username"] != "alice" {
			t.Fatal(acct)
		}
	})

	t.Run("relationships", func(t *testing.T) {
		rec := httptest.NewRecorder()
		url := "/api/v1/accounts/relationships?id[]=" + strconv.FormatInt(actorID, 10)
		mux.ServeHTTP(rec, newAuthedRequest(http.MethodGet, url, rawToken))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var rel []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rel); err != nil {
			t.Fatal(err)
		}
		if len(rel) != 1 {
			t.Fatalf("got %d elems", len(rel))
		}
	})

	t.Run("markers_get_post", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newAuthedRequest(http.MethodGet, "/api/v1/markers", rawToken))
		if rec.Code != http.StatusOK {
			t.Fatalf("get %d", rec.Code)
		}
		rec2 := httptest.NewRecorder()
		mux.ServeHTTP(rec2, newAuthedRequest(http.MethodPost, "/api/v1/markers", rawToken))
		if rec2.Code != http.StatusOK {
			t.Fatalf("post %d %s", rec2.Code, rec2.Body.String())
		}
	})

	t.Run("lists_get", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newAuthedRequest(http.MethodGet, "/api/v1/lists", rawToken))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var lists []any
		if err := json.Unmarshal(rec.Body.Bytes(), &lists); err != nil || len(lists) != 0 {
			t.Fatalf("body %s err %v", rec.Body.String(), err)
		}
	})

	t.Run("search_v2_accounts_leading_at", func(t *testing.T) {
		rec := httptest.NewRecorder()
		q := "/api/v2/search?q=" + url.QueryEscape("@alice@routes-int.test") + "&type=accounts"
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		accts, ok := doc["accounts"].([]any)
		if !ok || len(accts) < 1 {
			t.Fatalf("accounts: %#v", doc["accounts"])
		}
	})

	t.Run("search_v2_accounts", func(t *testing.T) {
		rec := httptest.NewRecorder()
		q := "/api/v2/search?q=" + url.QueryEscape("alice@routes-int.test") + "&type=accounts"
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		accts, ok := doc["accounts"].([]any)
		if !ok || len(accts) < 1 {
			t.Fatalf("accounts: %#v", doc["accounts"])
		}
	})

	t.Run("accounts_search_bare_handle_case_insensitive", func(t *testing.T) {
		rec := httptest.NewRecorder()
		path := "/api/v1/accounts/search?q=" + url.QueryEscape("ALICE")
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var accts []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &accts); err != nil {
			t.Fatal(err)
		}
		if len(accts) != 1 || accts[0]["username"] != "alice" {
			t.Fatalf("got %#v", accts)
		}
	})

	t.Run("search_v2_bare_local_handle", func(t *testing.T) {
		rec := httptest.NewRecorder()
		q := "/api/v2/search?q=" + url.QueryEscape("alice") + "&type=accounts"
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		accts, ok := doc["accounts"].([]any)
		if !ok || len(accts) != 1 {
			t.Fatalf("accounts: %#v", doc["accounts"])
		}
		first, ok := accts[0].(map[string]any)
		if !ok || first["username"] != "alice" {
			t.Fatalf("account: %#v", accts[0])
		}
	})

	t.Run("account_lookup", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/accounts/lookup?acct=alice@routes-int.test", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var acct map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &acct); err != nil {
			t.Fatal(err)
		}
		if acct["username"] != "alice" {
			t.Fatal(acct)
		}
	})

	t.Run("post_apps", func(t *testing.T) {
		body := `{"client_name":"x","redirect_uris":"https://cb.example/oauth"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/apps", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out["client_id"] == nil || out["client_id"] == "" {
			t.Fatal(out)
		}
	})

	t.Run("patch_update_credentials", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newAuthedRequest(http.MethodPatch, "/api/v1/accounts/update_credentials", rawToken))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("post_status_home_public_account_get_context", func(t *testing.T) {
		body := `{"status":"hello timeline read"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+rawToken)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("post %d %s", rec.Code, rec.Body.String())
		}
		var posted map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &posted); err != nil {
			t.Fatal(err)
		}
		statusID := fmt.Sprint(posted["id"])
		if statusID == "" || statusID == "0" {
			t.Fatalf("status id: %#v", posted["id"])
		}

		recHome := httptest.NewRecorder()
		mux.ServeHTTP(recHome, newAuthedRequest(http.MethodGet, "/api/v1/timelines/home", rawToken))
		if recHome.Code != http.StatusOK {
			t.Fatalf("home %d %s", recHome.Code, recHome.Body.String())
		}
		var homeList []map[string]any
		if err := json.Unmarshal(recHome.Body.Bytes(), &homeList); err != nil {
			t.Fatal(err)
		}
		foundHome := false
		for _, row := range homeList {
			if fmt.Sprint(row["id"]) == statusID {
				foundHome = true
				break
			}
		}
		if !foundHome {
			t.Fatalf("home timeline missing status %s: %s", statusID, recHome.Body.String())
		}

		recPub := httptest.NewRecorder()
		mux.ServeHTTP(recPub, httptest.NewRequest(http.MethodGet, "/api/v1/timelines/public", nil))
		if recPub.Code != http.StatusOK {
			t.Fatalf("public %d %s", recPub.Code, recPub.Body.String())
		}
		var pubList []map[string]any
		if err := json.Unmarshal(recPub.Body.Bytes(), &pubList); err != nil {
			t.Fatal(err)
		}
		foundPub := false
		for _, row := range pubList {
			if fmt.Sprint(row["id"]) == statusID {
				foundPub = true
				break
			}
		}
		if !foundPub {
			t.Fatalf("public timeline missing status %s: %s", statusID, recPub.Body.String())
		}

		acctPath := "/api/v1/accounts/" + strconv.FormatInt(actorID, 10) + "/statuses"
		recAcct := httptest.NewRecorder()
		mux.ServeHTTP(recAcct, httptest.NewRequest(http.MethodGet, acctPath, nil))
		if recAcct.Code != http.StatusOK {
			t.Fatalf("account statuses %d %s", recAcct.Code, recAcct.Body.String())
		}
		var acctStatuses []map[string]any
		if err := json.Unmarshal(recAcct.Body.Bytes(), &acctStatuses); err != nil {
			t.Fatal(err)
		}
		foundAcct := false
		for _, row := range acctStatuses {
			if fmt.Sprint(row["id"]) == statusID {
				foundAcct = true
				break
			}
		}
		if !foundAcct {
			t.Fatalf("account statuses missing %s: %s", statusID, recAcct.Body.String())
		}

		recOne := httptest.NewRecorder()
		mux.ServeHTTP(recOne, httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+statusID, nil))
		if recOne.Code != http.StatusOK {
			t.Fatalf("get status %d %s", recOne.Code, recOne.Body.String())
		}
		var one map[string]any
		if err := json.Unmarshal(recOne.Body.Bytes(), &one); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(one["id"]) != statusID {
			t.Fatalf("get status id mismatch: %#v", one["id"])
		}

		recCtx := httptest.NewRecorder()
		mux.ServeHTTP(recCtx, httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+statusID+"/context", nil))
		if recCtx.Code != http.StatusOK {
			t.Fatalf("context %d %s", recCtx.Code, recCtx.Body.String())
		}
	})
}

func TestIntegration_OAuthToken_flows(t *testing.T) {
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

	cfg := &config.Config{PublicBaseURL: "https://oauth-int.test", LocalUsername: "alice", LocalUsernames: []string{"alice"}}
	h, err := aphttp.New(cfg, aphttp.Deps{Store: st, Queue: sqlqueue.New(st.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := &Server{H: h, Pool: st.Pool}
	srv.mountMastodon(mux)

	actorID, ok := h.LocalActorID("alice")
	if !ok || actorID < 1 {
		t.Fatalf("local actor: id=%d ok=%v", actorID, ok)
	}

	app, err := store.InsertOAuthApplication(ctx, st.Pool, "ivorylike", "https://app.example/oauth", "", "read write follow")
	if err != nil {
		t.Fatal(err)
	}
	redirect := "https://app.example/oauth"

	mustCode := func() string {
		t.Helper()
		c, err := store.InsertAuthorizationCode(ctx, st.Pool, app.ID, actorID, redirect, "read write", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	t.Run("authorization_code_json_body", func(t *testing.T) {
		payload := map[string]string{
			"grant_type":    "authorization_code",
			"code":          mustCode(),
			"client_id":     app.ClientID,
			"client_secret": app.ClientSecret,
			"redirect_uri":  redirect,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var tok map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(tok["access_token"]) == "" {
			t.Fatal(tok)
		}
	})

	t.Run("authorization_code_form", func(t *testing.T) {
		v := url.Values{}
		v.Set("grant_type", "authorization_code")
		v.Set("code", mustCode())
		v.Set("client_id", app.ClientID)
		v.Set("client_secret", app.ClientSecret)
		v.Set("redirect_uri", redirect)
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("authorization_code_basic_auth", func(t *testing.T) {
		v := url.Values{}
		v.Set("grant_type", "authorization_code")
		v.Set("code", mustCode())
		v.Set("redirect_uri", redirect)
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(app.ClientID, app.ClientSecret)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("grant_type_case_insensitive", func(t *testing.T) {
		v := url.Values{}
		v.Set("grant_type", "Authorization_Code")
		v.Set("code", mustCode())
		v.Set("client_id", app.ClientID)
		v.Set("client_secret", app.ClientSecret)
		v.Set("redirect_uri", redirect)
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("client_credentials_json", func(t *testing.T) {
		payload := map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     app.ClientID,
			"client_secret": app.ClientSecret,
			"redirect_uri":  redirect,
			"scope":         "read",
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var tok map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
			t.Fatal(err)
		}
		rawTok, _ := tok["access_token"].(string)
		if rawTok == "" {
			t.Fatal(tok)
		}
		aid, _, _, err := store.ActorIDForAccessToken(ctx, st.Pool, rawTok)
		if err != nil {
			t.Fatal(err)
		}
		if aid != 0 {
			t.Fatalf("client_credentials token should not be bound to a user actor (got id %d)", aid)
		}
	})

	t.Run("client_credentials_default_scope_read", func(t *testing.T) {
		payload := map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     app.ClientID,
			"client_secret": app.ClientSecret,
			"redirect_uri":  redirect,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var tok map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(tok["scope"]) != "read" {
			t.Fatalf("scope: %#v", tok["scope"])
		}
	})

	t.Run("unsupported_grant_refresh_token", func(t *testing.T) {
		body := `{"grant_type":"refresh_token","refresh_token":"x"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var errDoc map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errDoc); err != nil {
			t.Fatal(err)
		}
		if errDoc["error"] != "unsupported_grant_type" {
			t.Fatal(errDoc)
		}
	})

	t.Run("client_credentials_invalid_scope", func(t *testing.T) {
		payload := map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     app.ClientID,
			"client_secret": app.ClientSecret,
			"redirect_uri":  redirect,
			"scope":         "admin:read",
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var errDoc map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errDoc); err != nil {
			t.Fatal(err)
		}
		if errDoc["error"] != "invalid_scope" {
			t.Fatal(errDoc)
		}
	})

	t.Run("json_missing_grant_type", func(t *testing.T) {
		body := `{"code":"x","client_id":"a","redirect_uri":"` + redirect + `"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestIntegration_MastodonUnfollow_removesFollowEdge(t *testing.T) {
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
		PublicBaseURL:  "https://unfollow-int.test",
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

	const rawToken = "unfollow-test-token"
	app, err := store.InsertOAuthApplication(ctx, st.Pool, "uf", "https://app/cb", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO oauth_access_tokens (token_hash, application_id, actor_id, scopes)
		VALUES ($1, $2, $3, 'read write')`, hashAccessToken(rawToken), app.ID, aliceID); err != nil {
		t.Fatal(err)
	}

	followActID := "https://unfollow-int.test/#/activities/seed-follow"
	if err := store.UpsertFollow(ctx, st.Pool, aliceID, bobID, followActID, store.FollowStateAccepted); err != nil {
		t.Fatal(err)
	}
	okFollow, err := store.FollowExistsBetween(ctx, st.Pool, aliceID, bobID)
	if err != nil || !okFollow {
		t.Fatalf("seed follow: ok=%v err=%v", okFollow, err)
	}

	rec := httptest.NewRecorder()
	unfollowPath := "/api/v1/accounts/" + strconv.FormatInt(bobID, 10) + "/unfollow"
	mux.ServeHTTP(rec, newAuthedRequest(http.MethodPost, unfollowPath, rawToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	still, err := store.FollowExistsBetween(ctx, st.Pool, aliceID, bobID)
	if err != nil || still {
		t.Fatalf("expected no follow after unfollow: still=%v err=%v", still, err)
	}

	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, newAuthedRequest(http.MethodPost, unfollowPath, rawToken))
	if rec2.Code != http.StatusOK {
		t.Fatalf("idempotent unfollow %d %s", rec2.Code, rec2.Body.String())
	}
}
