package aphttp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestLoadActorPublicKeyPEM_deriveFromPrivate(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "private.pem")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privDER := x509.MarshalPKCS1PrivateKey(key)
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ActorPrivateKeyPath: privPath}
	pemOut, err := loadActorPublicKeyPEM(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseRSAPublicKeyPEM([]byte(pemOut)); err != nil {
		t.Fatalf("derived PEM invalid: %v", err)
	}
}
