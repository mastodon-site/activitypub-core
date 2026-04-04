package fetch

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
)

func TestPublicKeyForKeyID(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr, err := actorkey.PublicKeyPEMFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		keyID := base + "/users/alice#main-key"
		actor := map[string]any{
			"@context": []string{"https://www.w3.org/ns/activitystreams"},
			"id":       base + "/users/alice",
			"type":     "Person",
			"publicKey": map[string]any{
				"id":           keyID,
				"owner":        base + "/users/alice",
				"type":         "Key",
				"publicKeyPem": pemStr,
			},
		}
		j, err := json.Marshal(actor)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(j)
	}))
	defer srv.Close()

	client := srv.Client()
	keyID := srv.URL + "/users/alice#main-key"
	got, err := PublicKeyForKeyID(context.Background(), client, LaxPolicyForTests(), keyID)
	if err != nil {
		t.Fatal(err)
	}
	if got.N.Cmp(priv.PublicKey.N) != 0 {
		t.Fatal("public key mismatch")
	}
}

func TestPublicKeyForKeyID_keyIdMismatch(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr, err := actorkey.PublicKeyPEMFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		actor := map[string]any{
			"id": base + "/users/bob",
			"publicKey": map[string]any{
				"id":           base + "/users/bob#other-key",
				"publicKeyPem": pemStr,
			},
		}
		j, _ := json.Marshal(actor)
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(j)
	}))
	defer srv.Close()

	client := srv.Client()
	wrongKey := srv.URL + "/users/bob#main-key"
	if _, err := PublicKeyForKeyID(context.Background(), client, LaxPolicyForTests(), wrongKey); err == nil {
		t.Fatal("expected error when publicKey.id does not match keyId")
	}
}

func TestPublicKeyForKeyID_nonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	if _, err := PublicKeyForKeyID(context.Background(), client, LaxPolicyForTests(), srv.URL+"/users/x#main"); err == nil {
		t.Fatal("expected error on 404")
	}
}

// Regression (security): default fetch policy must block keyId URLs that resolve to local/SSRF targets.
func TestPublicKeyForKeyID_rejectsDisallowedHostUnderStrictPolicy(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr, err := actorkey.PublicKeyPEMFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		keyID := base + "/users/alice#main-key"
		actor := map[string]any{
			"id": base + "/users/alice",
			"publicKey": map[string]any{
				"id":           keyID,
				"publicKeyPem": pemStr,
			},
		}
		j, _ := json.Marshal(actor)
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(j)
	}))
	defer srv.Close()

	strict := &Policy{}
	keyID := srv.URL + "/users/alice#main-key"
	if _, err := PublicKeyForKeyID(context.Background(), srv.Client(), strict, keyID); err == nil {
		t.Fatal("strict policy must reject httptest loopback keyId before any useful fetch")
	}
}
