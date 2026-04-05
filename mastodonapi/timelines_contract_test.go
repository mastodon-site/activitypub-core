package mastodonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestContract_getTimelinePublic_returnsJSONArray(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/timelines/public?limit=20&local=true", nil)
	rr := httptest.NewRecorder()
	s.getTimelinePublic(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct == "" || ct[:16] != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var out []any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("want non-nil empty slice encoded as []")
	}
}

func TestContract_getTimelinePublic_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timelines/public", nil)
	rr := httptest.NewRecorder()
	s.getTimelinePublic(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}
