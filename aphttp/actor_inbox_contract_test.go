package aphttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestContract_GetLocalActor_perActorInboxDistinctFromSharedInbox(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ap.test",
		LocalUsername:  "carol",
		LocalUsernames: []string{"carol", "dave"},
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	req := httptest.NewRequest(http.MethodGet, "https://ap.test/@carol", nil)
	req.Header.Set("Accept", "application/activity+json")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	per := "https://ap.test/@carol/inbox"
	if doc["inbox"] != per {
		t.Fatalf("inbox field: got %#v want %q", doc["inbox"], per)
	}
	ep, ok := doc["endpoints"].(map[string]any)
	if !ok {
		t.Fatalf("endpoints: %#v", doc["endpoints"])
	}
	shared := "https://ap.test/inbox"
	if ep["sharedInbox"] != shared {
		t.Fatalf("sharedInbox: got %#v want %q", ep["sharedInbox"], shared)
	}
	if doc["inbox"] == ep["sharedInbox"] {
		t.Fatal("actor inbox and sharedInbox must differ")
	}
}

func TestContract_GetLocalActor_secondUser_inboxUsesUsername(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ap.test",
		LocalUsername:  "carol",
		LocalUsernames: []string{"carol", "dave"},
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	req := httptest.NewRequest(http.MethodGet, "https://ap.test/@dave", nil)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if want := "https://ap.test/@dave/inbox"; doc["inbox"] != want {
		t.Fatalf("got %#v", doc["inbox"])
	}
}
