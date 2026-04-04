package aphttp

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
)

func TestSharedInbox_acceptsSignedActivity(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr, err := actorkey.PublicKeyPEMFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}

	actorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		keyID := base + "/users/remote#main-key"
		actor := map[string]any{
			"@context": []string{"https://www.w3.org/ns/activitystreams"},
			"id":       base + "/users/remote",
			"type":     "Person",
			"publicKey": map[string]any{
				"id":           keyID,
				"owner":        base + "/users/remote",
				"type":         "Key",
				"publicKeyPem": pemStr,
			},
		}
		j, err := json.Marshal(actor)
		if err != nil {
			t.Error(err)
			http.Error(w, "err", 500)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(j)
	}))
	defer actorSrv.Close()
	keyID := actorSrv.URL + "/users/remote#main-key"

	h, err := New(&config.Config{InboxMaxBody: 65536}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	h.fetchClient = actorSrv.Client()

	body, err := json.Marshal(map[string]any{
		"type":  "Create",
		"id":    "https://sender.test/o/1",
		"actor": actorSrv.URL + "/users/remote",
	})
	if err != nil {
		t.Fatal(err)
	}
	inboxURL := "https://destination.test/inbox"
	req, err := httpsig.NewSignedPost(inboxURL, body, keyID, priv)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
}

func TestSharedInbox_rejectsMissingSignature(t *testing.T) {
	h, err := New(&config.Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/inbox", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestSharedInbox_rejectsWrongContentType(t *testing.T) {
	h, err := New(&config.Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/inbox", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestSharedInbox_rejectsOversizedBody(t *testing.T) {
	h, err := New(&config.Config{InboxMaxBody: 8}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/inbox", strings.NewReader(`123456789`))
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestIsActivityJSONContentType(t *testing.T) {
	if !isActivityJSONContentType("application/activity+json") {
		t.Fatal()
	}
	if !isActivityJSONContentType(`application/ld+json; profile="https://www.w3.org/ns/activitystreams"`) {
		t.Fatal()
	}
	if isActivityJSONContentType("text/plain") {
		t.Fatal()
	}
	if isActivityJSONContentType("") {
		t.Fatal()
	}
}
