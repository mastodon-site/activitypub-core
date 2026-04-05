package mastodonapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("AP_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("set AP_TEST_DATABASE_URL for route integration tests")
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
	h, err := aphttp.New(cfg, aphttp.Deps{Store: st})
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
}
