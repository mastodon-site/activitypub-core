package aphttp

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
)

func TestContract_SharedInbox_methodNotAllowed(t *testing.T) {
	h, err := New(&config.Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/inbox", nil)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestContract_SharedInbox_invalidSignatureHeader(t *testing.T) {
	h, err := New(&config.Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/inbox", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Signature", "not-a-valid-header")
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_missingKeyId(t *testing.T) {
	h, err := New(&config.Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/inbox", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Signature", `keyId=""`)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestContract_SharedInbox_keyResolutionFailure(t *testing.T) {
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = http.DefaultClient

	body := []byte(`{"type":"Create","id":"https://x/y","actor":"https://x/a"}`)
	req := httptest.NewRequest(http.MethodPost, "https://inbox.test/inbox", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Signature", `keyId="https://no-such-host.invalid/user#main",algorithm="rsa-sha256",headers="(request-target) host date digest content-type",signature="AAA="`)

	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestContract_SharedInbox_activityFieldsMissing_id(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	body := mustJSON(t, map[string]any{
		"type":  "Create",
		"actor": strings.TrimSuffix(fix.KeyID, "#main-key"),
	})
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "id") {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_activityFieldsMissing_type(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	body := mustJSON(t, map[string]any{
		"id":    "https://sender.test/o/missing-type",
		"actor": strings.TrimSuffix(fix.KeyID, "#main-key"),
	})
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "type") {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_activityFieldsMissing_actor(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	body := mustJSON(t, map[string]any{
		"type": "Create",
		"id":   "https://sender.test/o/no-actor",
	})
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "actor") {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_actorDoesNotMatchSignerKeyId(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	body := mustJSON(t, map[string]any{
		"type":  "Create",
		"id":    "https://sender.test/o/1",
		"actor": "https://someone-else.example/users/bob",
	})
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_invalidJSONBody(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	body := []byte(`{not-json`)
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid json after verify, got %d %q", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_acceptsLdJSONContentType(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	body := mustJSON(t, map[string]any{
		"type":  "Create",
		"id":    "https://sender.test/o/ld-ct",
		"actor": strings.TrimSuffix(fix.KeyID, "#main-key"),
	})
	inboxURL := "https://destination.test/inbox"
	u, err := url.Parse(inboxURL)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.URL = u
	req.Host = u.Host
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`)
	if err := httpsig.SignPost(req, body, fix.KeyID, fix.Priv); err != nil {
		t.Fatal(err)
	}
	// Guard: signed Date header must be fresh for VerifyRequest clock skew
	if req.Header.Get("Date") == "" {
		t.Fatal("missing Date")
	}

	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_acceptsActorAsObject(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	actorBase := strings.TrimSuffix(fix.KeyID, "#main-key")
	body := mustJSON(t, map[string]any{
		"type": "Follow",
		"id":   "https://sender.test/o/follow-obj",
		"actor": map[string]any{
			"id":   actorBase,
			"type": "Person",
		},
	})
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestContract_SharedInbox_acceptsJsonLdTypeArray(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	actorBase := strings.TrimSuffix(fix.KeyID, "#main-key")
	body := []byte(`{"@context":"https://www.w3.org/ns/activitystreams","type":["https://www.w3.org/ns/activitystreams#Create","Create"],"id":"https://sender.test/o/typed","actor":` + mustMarshalString(t, actorBase) + `}`)
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func mustMarshalString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestContract_SharedInbox_responseBodyEmptyOn202(t *testing.T) {
	fix := newActorFixture(t)
	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = fix.Client

	body := mustJSON(t, map[string]any{
		"type":  "Like",
		"id":    "https://sender.test/o/like-1",
		"actor": strings.TrimSuffix(fix.KeyID, "#main-key"),
	})
	req := mustSignedPost(t, "https://inbox.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatal(rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "" {
		t.Fatalf("expected empty body, got %q", rr.Body.String())
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustSignedPost(t *testing.T, inboxURL string, body []byte, keyID string, priv *rsa.PrivateKey) *http.Request {
	t.Helper()
	req, err := httpsig.NewSignedPost(inboxURL, body, keyID, priv)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
