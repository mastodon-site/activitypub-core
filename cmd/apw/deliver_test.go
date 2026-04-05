package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
	"github.com/mastodon-site/activitypub-core/internal/worker"
)

func TestDeliverActivity_POSTsValidSignature(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "priv.pem")
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	inboxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		pub := &priv.PublicKey
		if err := httpsig.VerifyRequest(r, body, pub); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer inboxSrv.Close()

	cfg := &config.Config{
		PublicBaseURL:       "https://origin.example",
		LocalUsername:       "admin",
		ActorPrivateKeyPath: keyPath,
	}
	testClient := fetch.NewHTTPClientForPolicy(fetch.TestingPolicy(), 60*time.Second)
	act := json.RawMessage(`{"type":"Create","id":"https://origin.example/o/1"}`)
	raw, err := json.Marshal(map[string]any{"inboxUrl": inboxSrv.URL, "body": act})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.DeliverActivity(context.Background(), cfg, nil, testClient, raw); err != nil {
		t.Fatal(err)
	}
}

func TestDeliverActivity_requiresKeys(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://x"}
	raw, _ := json.Marshal(map[string]any{"inboxUrl": "http://y", "body": json.RawMessage(`{}`)})
	if err := worker.DeliverActivity(context.Background(), cfg, nil, nil, raw); err == nil {
		t.Fatal("expected error without private key path")
	}
}
