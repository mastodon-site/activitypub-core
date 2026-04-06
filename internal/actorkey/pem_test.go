package actorkey

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

func TestPublicKeyPEMFromPrivate_roundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr, err := PublicKeyPEMFromPrivate(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKeyPEM([]byte(pemStr)); err != nil {
		t.Fatal(err)
	}
}

func TestActorPublicKeyPEMForConfig_derive(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "private.pem")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ActorPrivateKeyPath: privPath}
	out, err := ActorPublicKeyPEMForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKeyPEM([]byte(out)); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateRSA2048KeyPair_PrivateKeyToPKCS8PEM_roundTrip(t *testing.T) {
	priv, err := GenerateRSA2048KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := PrivateKeyToPKCS8PEM(priv)
	if err != nil {
		t.Fatal(err)
	}
	priv2, err := ParsePrivateKeyPEM(raw)
	if err != nil {
		t.Fatal(err)
	}
	if priv2.PublicKey.N.Cmp(priv.PublicKey.N) != 0 {
		t.Fatal("key mismatch")
	}
}
