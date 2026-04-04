package mastodonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestContract_Instance_hasUriTitleVersion(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instance", nil)
	rr := httptest.NewRecorder()
	s.getInstance(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"uri", "title", "version"} {
		if s, ok := doc[k].(string); !ok || s == "" {
			t.Fatalf("missing string %s in %v", k, doc)
		}
	}
	for _, k := range []string{"stats", "configuration", "languages"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("missing key %s in %v", k, doc)
		}
	}
}

func TestContract_InstanceV2_hasDomainConfiguration(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/instance", nil)
	rr := httptest.NewRecorder()
	s.getInstanceV2(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"domain", "title", "version", "configuration", "api_versions"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("missing %s in %v", k, doc)
		}
	}
}

func TestContract_OAuthAuthorizationServerMetadata_JSON(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rr := httptest.NewRecorder()
	s.getOAuthAuthorizationServer(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"issuer", "authorization_endpoint", "token_endpoint"} {
		if s, ok := doc[k].(string); !ok || s == "" {
			t.Fatalf("missing string %s in %v", k, doc)
		}
	}
}

func TestContract_CustomEmojis_emptyArray(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://inst.test", LocalUsername: "x"}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/custom_emojis", nil)
	rr := httptest.NewRecorder()
	s.getCustomEmojis(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "[]" {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
}
