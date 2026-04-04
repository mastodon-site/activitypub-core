package aphttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestContract_perActorAtPrefixInbox_acceptsSignedPost(t *testing.T) {
	fix := newActorFixture(t)
	cfg := &config.Config{
		PublicBaseURL:  "https://inbox.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
		InboxMaxBody:   65536,
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	h.fetchClient = fix.Client
	th := testMounted(h)

	body := mustJSON(t, map[string]any{
		"type":  "Create",
		"id":    "https://sender.test/o/at-inbox-contract",
		"actor": strings.TrimSuffix(fix.KeyID, "#main-key"),
	})
	req := mustSignedPost(t, "https://inbox.test/@alice/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_legacyUsersPathInbox_acceptsSignedPost(t *testing.T) {
	fix := newActorFixture(t)
	cfg := &config.Config{
		PublicBaseURL:  "https://inbox.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
		InboxMaxBody:   65536,
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	h.fetchClient = fix.Client
	th := testMounted(h)

	body := mustJSON(t, map[string]any{
		"type":  "Create",
		"id":    "https://sender.test/o/users-inbox-contract",
		"actor": strings.TrimSuffix(fix.KeyID, "#main-key"),
	})
	req := mustSignedPost(t, "https://inbox.test/users/alice/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_atPrefixInbox_GET_localUser_methodNotAllowed(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ap.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	req := httptest.NewRequest(http.MethodGet, "https://ap.test/@alice/inbox", nil)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestContract_atPrefixInbox_GET_unknownUser_notFound(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ap.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	req := httptest.NewRequest(http.MethodGet, "https://ap.test/@nobody/inbox", nil)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestContract_atPrefixInbox_POST_unknownUser_notFound(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ap.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	req := httptest.NewRequest(http.MethodPost, "https://ap.test/@nobody/inbox", nil)
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
}

func TestContract_legacyUsersPathInbox_POST_unknownUser_notFound(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ap.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	req := httptest.NewRequest(http.MethodPost, "https://ap.test/users/nope/inbox", nil)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestContract_legacyUsersPathInbox_GET_noDedicatedRoute(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ap.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
	}
	h, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	// Only POST is registered for /users/{username}/inbox; GET falls through to the activity catch-all.
	// Without a store, the catch-all cannot resolve this path.
	req := httptest.NewRequest(http.MethodGet, "https://ap.test/users/alice/inbox", nil)
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
}
