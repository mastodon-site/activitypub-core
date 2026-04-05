package mastodonapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
)

// Exercise the mounted Mastodon routes without a database. Any request that sends a
// non-empty Bearer token would hit the DB — those cases must omit Authorization.
func TestMastodonRoutes_publicAndUnauthorized(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "alice"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s := &Server{H: h, Pool: nil}
	s.mountMastodon(mux)

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		check    func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "oauth metadata",
			method: http.MethodGet, path: "/.well-known/oauth-authorization-server", wantCode: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var m map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
					t.Fatal(err)
				}
				if m["issuer"] == nil || m["issuer"] == "" {
					t.Fatalf("issuer missing: %#v", m)
				}
			},
		},
		{name: "instance v1", method: http.MethodGet, path: "/api/v1/instance", wantCode: http.StatusOK},
		{name: "instance v2", method: http.MethodGet, path: "/api/v2/instance", wantCode: http.StatusOK},
		{name: "instance peers", method: http.MethodGet, path: "/api/v1/instance/peers", wantCode: http.StatusOK},
		{name: "instance rules", method: http.MethodGet, path: "/api/v1/instance/rules", wantCode: http.StatusOK},
		{name: "instance translation_languages", method: http.MethodGet, path: "/api/v1/instance/translation_languages", wantCode: http.StatusOK},
		{name: "instance domain_blocks", method: http.MethodGet, path: "/api/v1/instance/domain_blocks", wantCode: http.StatusOK},
		{name: "instance activity", method: http.MethodGet, path: "/api/v1/instance/activity", wantCode: http.StatusOK},
		{name: "extended description", method: http.MethodGet, path: "/api/v1/instance/extended_description", wantCode: http.StatusOK},
		{name: "v2 search", method: http.MethodGet, path: "/api/v2/search?q=", wantCode: http.StatusOK},
		{name: "v2 filters", method: http.MethodGet, path: "/api/v2/filters", wantCode: http.StatusOK},
		{name: "v2 suggestions", method: http.MethodGet, path: "/api/v2/suggestions", wantCode: http.StatusOK},
		{name: "search empty", method: http.MethodGet, path: "/api/v1/search", wantCode: http.StatusOK},
		{name: "search statuses only type", method: http.MethodGet, path: "/api/v1/search?q=x&type=statuses", wantCode: http.StatusOK},
		{name: "timelines public", method: http.MethodGet, path: "/api/v1/timelines/public", wantCode: http.StatusOK},
		{name: "timeline tag", method: http.MethodGet, path: "/api/v1/timelines/tag/foo", wantCode: http.StatusOK},
		{name: "timeline list", method: http.MethodGet, path: "/api/v1/timelines/list/1", wantCode: http.StatusOK},
		{name: "directory", method: http.MethodGet, path: "/api/v1/directory", wantCode: http.StatusOK},
		{name: "suggestions v1", method: http.MethodGet, path: "/api/v1/suggestions", wantCode: http.StatusOK},
		{name: "streaming not implemented", method: http.MethodGet, path: "/api/v1/streaming", wantCode: http.StatusNotImplemented},
		{name: "custom emojis", method: http.MethodGet, path: "/api/v1/custom_emojis", wantCode: http.StatusOK},
		{name: "announcements", method: http.MethodGet, path: "/api/v1/announcements", wantCode: http.StatusOK},
		{name: "tag", method: http.MethodGet, path: "/api/v1/tags/bar", wantCode: http.StatusOK},
		{name: "status context", method: http.MethodGet, path: "/api/v1/statuses/42/context", wantCode: http.StatusOK},
		{name: "status get 404", method: http.MethodGet, path: "/api/v1/statuses/42", wantCode: http.StatusNotFound},
		{name: "account search empty", method: http.MethodGet, path: "/api/v1/accounts/search", wantCode: http.StatusOK},
		{name: "account followers stub", method: http.MethodGet, path: "/api/v1/accounts/7/followers", wantCode: http.StatusOK},
		{name: "account statuses empty", method: http.MethodGet, path: "/api/v1/accounts/7/statuses", wantCode: http.StatusOK},
		{name: "poll 404", method: http.MethodGet, path: "/api/v1/polls/1", wantCode: http.StatusNotFound},
		{name: "media 404", method: http.MethodGet, path: "/api/v1/media/1", wantCode: http.StatusNotFound},
		{name: "filters list v1", method: http.MethodGet, path: "/api/v1/filters", wantCode: http.StatusOK},
		{name: "domain blocks", method: http.MethodGet, path: "/api/v1/domain_blocks", wantCode: http.StatusOK},
		{name: "trends tags", method: http.MethodGet, path: "/api/v1/trends/tags", wantCode: http.StatusOK},
		{name: "follow requests empty", method: http.MethodGet, path: "/api/v1/follow_requests", wantCode: http.StatusOK},
		{name: "conversations list", method: http.MethodGet, path: "/api/v1/conversations", wantCode: http.StatusOK},
		{name: "featured tags list", method: http.MethodGet, path: "/api/v1/featured_tags", wantCode: http.StatusOK},
		{name: "endorsements", method: http.MethodGet, path: "/api/v1/endorsements", wantCode: http.StatusOK},
		{name: "followed tags", method: http.MethodGet, path: "/api/v1/followed_tags", wantCode: http.StatusOK},
		{name: "admin forbidden", method: http.MethodGet, path: "/api/v1/admin/foo", wantCode: http.StatusForbidden},
		{name: "oauth revoke", method: http.MethodPost, path: "/oauth/revoke", wantCode: http.StatusOK},

		// Unauthenticated bearer-only routes (must not send Authorization — would touch nil pool).
		{name: "verify_credentials 401", method: http.MethodGet, path: "/api/v1/accounts/verify_credentials", wantCode: http.StatusUnauthorized},
		{name: "notifications 401", method: http.MethodGet, path: "/api/v1/notifications", wantCode: http.StatusUnauthorized},
		{name: "timelines home 401", method: http.MethodGet, path: "/api/v1/timelines/home", wantCode: http.StatusUnauthorized},
		{name: "post statuses 401", method: http.MethodPost, path: "/api/v1/statuses", wantCode: http.StatusUnauthorized},

		// Wrong method
		{name: "instance POST 405", method: http.MethodPost, path: "/api/v1/instance", wantCode: http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.method == http.MethodPost && tc.path == "/oauth/revoke" {
				req.Body = io.NopCloser(strings.NewReader("token=x"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
			if tc.check != nil {
				tc.check(t, rec)
			}
		})
	}
}

func TestMastodonRoutes_instanceV2_hasAPIVersions(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/instance", nil)
	mux := http.NewServeMux()
	(&Server{H: h, Pool: nil}).mountMastodon(mux)
	mux.ServeHTTP(rec, req)
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	av, ok := doc["api_versions"].(map[string]any)
	if !ok {
		t.Fatalf("api_versions: %#v", doc["api_versions"])
	}
	switch v := av["mastodon"].(type) {
	case float64:
		if v != 1 {
			t.Fatalf("api_versions.mastodon %v", v)
		}
	default:
		t.Fatalf("api_versions.mastodon type %T val %#v", av["mastodon"], av["mastodon"])
	}
}
