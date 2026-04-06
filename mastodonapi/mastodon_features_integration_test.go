package mastodonapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	fsblob "github.com/mastodon-site/activitypub-core/blobs/fs"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

// TestIntegration_MastodonFeatures_mediaContextDeleteListFilter exercises Mastodon API extensions
// against Postgres (requires AP_TEST_DATABASE_URL).
func TestIntegration_MastodonFeatures_mediaContextDeleteListFilter(t *testing.T) {
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
		PublicBaseURL:  "https://feat-int.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice", "bob"},
	}
	blobRoot := t.TempDir()
	deps := aphttp.Deps{
		Store:       st,
		Queue:       sqlqueue.New(st.Pool),
		Blobs:       fsblob.New(blobRoot),
		FetchPolicy: fetch.TestingPolicy(),
		FetchClient: mastodonIntegrationStubFetchClient(cfg.PublicBaseURL),
	}
	h, err := aphttp.New(cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Server{H: h, Pool: st.Pool}).mountMastodon(mux)

	aliceID, _ := h.LocalActorID("alice")
	bobID, _ := h.LocalActorID("bob")
	app, err := store.InsertOAuthApplication(ctx, st.Pool, "f", "https://app/cb", "", "read write")
	if err != nil {
		t.Fatal(err)
	}
	const tokAlice = "feat-int-alice"
	const tokBob = "feat-int-bob"
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

	// Upload media (multipart)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("fakepng")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	reqM := httptest.NewRequest(http.MethodPost, "/api/v1/media", &buf)
	reqM.Header.Set("Content-Type", mw.FormDataContentType())
	reqM.Header.Set("Authorization", "Bearer "+tokAlice)
	recM := httptest.NewRecorder()
	mux.ServeHTTP(recM, reqM)
	if recM.Code != http.StatusOK {
		t.Fatalf("media upload: %d %s", recM.Code, recM.Body.String())
	}
	var media map[string]any
	if err := json.Unmarshal(recM.Body.Bytes(), &media); err != nil {
		t.Fatal(err)
	}
	midStr, _ := media["id"].(string)
	mid, _ := strconv.ParseInt(midStr, 10, 64)
	if mid < 1 {
		t.Fatalf("media id: %#v", media)
	}

	// Post with media
	body := `{"status":"hello pic","media_ids":[` + midStr + `]}`
	reqP := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(body))
	reqP.Header.Set("Content-Type", "application/json")
	reqP.Header.Set("Authorization", "Bearer "+tokAlice)
	recP := httptest.NewRecorder()
	mux.ServeHTTP(recP, reqP)
	if recP.Code != http.StatusOK {
		t.Fatalf("post status: %d %s", recP.Code, recP.Body.String())
	}
	var stDoc map[string]any
	if err := json.Unmarshal(recP.Body.Bytes(), &stDoc); err != nil {
		t.Fatal(err)
	}
	rootID := fmtInt64MapKey(stDoc["id"])
	if atts, _ := stDoc["media_attachments"].([]any); len(atts) != 1 {
		t.Fatalf("media_attachments: %#v", stDoc["media_attachments"])
	}

	// Reply for context
	body2 := `{"status":"reply here","in_reply_to_id":` + rootID + `}`
	reqR := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(body2))
	reqR.Header.Set("Content-Type", "application/json")
	reqR.Header.Set("Authorization", "Bearer "+tokBob)
	recR := httptest.NewRecorder()
	mux.ServeHTTP(recR, reqR)
	if recR.Code != http.StatusOK {
		t.Fatalf("reply: %d %s", recR.Code, recR.Body.String())
	}
	var replyDoc map[string]any
	if err := json.Unmarshal(recR.Body.Bytes(), &replyDoc); err != nil {
		t.Fatal(err)
	}
	replyID := fmtInt64MapKey(replyDoc["id"])

	reqC := httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+replyID+"/context", nil)
	recC := httptest.NewRecorder()
	mux.ServeHTTP(recC, reqC)
	if recC.Code != http.StatusOK {
		t.Fatalf("context: %d %s", recC.Code, recC.Body.String())
	}
	var ctxDoc map[string]any
	if err := json.Unmarshal(recC.Body.Bytes(), &ctxDoc); err != nil {
		t.Fatal(err)
	}
	anc, _ := ctxDoc["ancestors"].([]any)
	desc, _ := ctxDoc["descendants"].([]any)
	if len(anc) != 1 || len(desc) != 0 {
		t.Fatalf("ancestors=%d descendants=%d", len(anc), len(desc))
	}

	// Delete root (alice)
	reqD := httptest.NewRequest(http.MethodDelete, "/api/v1/statuses/"+rootID, nil)
	reqD.Header.Set("Authorization", "Bearer "+tokAlice)
	recD := httptest.NewRecorder()
	mux.ServeHTTP(recD, reqD)
	if recD.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", recD.Code, recD.Body.String())
	}
	reqG := httptest.NewRequest(http.MethodGet, "/api/v1/statuses/"+rootID, nil)
	recG := httptest.NewRecorder()
	mux.ServeHTTP(recG, reqG)
	if recG.Code != http.StatusNotFound {
		t.Fatalf("get deleted want 404 got %d", recG.Code)
	}

	// List + timeline
	title := `{"title":"Close friends"}`
	reqL := httptest.NewRequest(http.MethodPost, "/api/v1/lists", strings.NewReader(title))
	reqL.Header.Set("Content-Type", "application/json")
	reqL.Header.Set("Authorization", "Bearer "+tokAlice)
	recL := httptest.NewRecorder()
	mux.ServeHTTP(recL, reqL)
	if recL.Code != http.StatusOK {
		t.Fatalf("post list: %d %s", recL.Code, recL.Body.String())
	}
	var listDoc map[string]any
	if err := json.Unmarshal(recL.Body.Bytes(), &listDoc); err != nil {
		t.Fatal(err)
	}
	listID, _ := listDoc["id"].(string)

	addAcc := `{"account_ids":[` + strconv.FormatInt(bobID, 10) + `]}`
	reqA := httptest.NewRequest(http.MethodPost, "/api/v1/lists/"+listID+"/accounts", strings.NewReader(addAcc))
	reqA.Header.Set("Content-Type", "application/json")
	reqA.Header.Set("Authorization", "Bearer "+tokAlice)
	recA := httptest.NewRecorder()
	mux.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("add list account: %d %s", recA.Code, recA.Body.String())
	}

	// Bob posts for list TL
	socialExtMustPostStatus(t, mux, tokBob, `{"status":"for list tl"}`)
	reqTL := httptest.NewRequest(http.MethodGet, "/api/v1/timelines/list/"+listID, nil)
	reqTL.Header.Set("Authorization", "Bearer "+tokAlice)
	recTL := httptest.NewRecorder()
	mux.ServeHTTP(recTL, reqTL)
	if recTL.Code != http.StatusOK {
		t.Fatalf("list tl: %d %s", recTL.Code, recTL.Body.String())
	}
	var tl []any
	if err := json.Unmarshal(recTL.Body.Bytes(), &tl); err != nil || len(tl) < 1 {
		t.Fatalf("list timeline: %v", recTL.Body.String())
	}

	socialExtMustPostStatus(t, mux, tokAlice, `{"status":"clean post"}`)
	fb := `{"phrase":"spamword","context":["home"]}`
	reqF := httptest.NewRequest(http.MethodPost, "/api/v1/filters", strings.NewReader(fb))
	reqF.Header.Set("Content-Type", "application/json")
	reqF.Header.Set("Authorization", "Bearer "+tokAlice)
	recF := httptest.NewRecorder()
	mux.ServeHTTP(recF, reqF)
	if recF.Code != http.StatusOK {
		t.Fatalf("post filter: %d %s", recF.Code, recF.Body.String())
	}
	socialExtMustPostStatus(t, mux, tokAlice, `{"status":"has spamword in it"}`)
	reqH := httptest.NewRequest(http.MethodGet, "/api/v1/timelines/home", nil)
	reqH.Header.Set("Authorization", "Bearer "+tokAlice)
	recH := httptest.NewRecorder()
	mux.ServeHTTP(recH, reqH)
	if recH.Code != http.StatusOK {
		t.Fatalf("home: %d %s", recH.Code, recH.Body.String())
	}
	var home []any
	_ = json.Unmarshal(recH.Body.Bytes(), &home)
	for _, e := range home {
		m := e.(map[string]any)
		c, _ := m["content"].(string)
		if strings.Contains(strings.ToLower(c), "spamword") {
			t.Fatalf("filter should hide status: %#v", c)
		}
	}

	// Direct message
	dir := `{"status":"secret","visibility":"direct","direct_account_ids":[` + strconv.FormatInt(bobID, 10) + `]}`
	reqDir := httptest.NewRequest(http.MethodPost, "/api/v1/statuses", strings.NewReader(dir))
	reqDir.Header.Set("Content-Type", "application/json")
	reqDir.Header.Set("Authorization", "Bearer "+tokAlice)
	recDir := httptest.NewRecorder()
	mux.ServeHTTP(recDir, reqDir)
	if recDir.Code != http.StatusOK {
		t.Fatalf("direct: %d %s", recDir.Code, recDir.Body.String())
	}
	reqConv := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	reqConv.Header.Set("Authorization", "Bearer "+tokBob)
	recConv := httptest.NewRecorder()
	mux.ServeHTTP(recConv, reqConv)
	if recConv.Code != http.StatusOK {
		t.Fatalf("conversations: %d %s", recConv.Code, recConv.Body.String())
	}
	var convs []any
	if err := json.Unmarshal(recConv.Body.Bytes(), &convs); err != nil || len(convs) < 1 {
		t.Fatalf("conversations body: %s", recConv.Body.String())
	}
}

func fmtInt64MapKey(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case string:
		return x
	default:
		return ""
	}
}
