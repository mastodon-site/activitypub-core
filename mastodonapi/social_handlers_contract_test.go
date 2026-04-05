package mastodonapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestContract_getAccountFollowers_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/1/followers", nil)
	req.SetPathValue("id", "1")
	rr := httptest.NewRecorder()
	s.getAccountFollowers(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_getAccountFollowing_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/accounts/1/following", nil)
	req.SetPathValue("id", "1")
	rr := httptest.NewRecorder()
	s.getAccountFollowing(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_getAccountStatuses_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/accounts/1/statuses", nil)
	req.SetPathValue("id", "1")
	rr := httptest.NewRecorder()
	s.getAccountStatuses(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_getStatus_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/statuses/1", nil)
	req.SetPathValue("id", "1")
	rr := httptest.NewRecorder()
	s.getStatus(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_getTimelineHome_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timelines/home", nil)
	rr := httptest.NewRecorder()
	s.getTimelineHome(rr, req, 1)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_postStatuses_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statuses", nil)
	rr := httptest.NewRecorder()
	s.postStatuses(rr, req, 1)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_getStatusContext_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/statuses/1/context", nil)
	req.SetPathValue("id", "1")
	rr := httptest.NewRecorder()
	s.getStatusContext(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}
