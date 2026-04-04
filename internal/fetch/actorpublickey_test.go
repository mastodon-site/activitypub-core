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
	got, err := PublicKeyForKeyID(context.Background(), client, keyID)
	if err != nil {
		t.Fatal(err)
	}
	if got.N.Cmp(priv.PublicKey.N) != 0 {
		t.Fatal("public key mismatch")
	}
}
