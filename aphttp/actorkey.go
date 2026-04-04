package aphttp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// loadActorPublicKeyPEM returns PKIX PEM text for the actor's publicKey.publicKeyPem field.
func loadActorPublicKeyPEM(cfg *config.Config) (string, error) {
	if cfg.ActorPrivateKeyPath == "" {
		return "", fmt.Errorf("actor private key path empty")
	}
	privRaw, err := os.ReadFile(cfg.ActorPrivateKeyPath)
	if err != nil {
		return "", fmt.Errorf("read actor private key: %w", err)
	}
	if cfg.ActorPublicKeyPath != "" {
		pubRaw, err := os.ReadFile(cfg.ActorPublicKeyPath)
		if err != nil {
			return "", fmt.Errorf("read actor public key: %w", err)
		}
		if _, err := parseRSAPublicKeyPEM(pubRaw); err != nil {
			return "", fmt.Errorf("parse actor public key: %w", err)
		}
		return string(pubRaw), nil
	}
	priv, err := parseRSAPrivateKeyPEM(privRaw)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

func parseRSAPrivateKeyPEM(raw []byte) (*rsa.PrivateKey, error) {
	var block *pem.Block
	for len(raw) > 0 {
		block, raw = pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("no PEM block in actor private key")
		}
		switch block.Type {
		case "RSA PRIVATE KEY":
			k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse PKCS#1 private key: %w", err)
			}
			return k, nil
		case "PRIVATE KEY":
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
			}
			rsaKey, ok := key.(*rsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("PKCS#8 key is %T, want RSA", key)
			}
			return rsaKey, nil
		}
	}
	return nil, fmt.Errorf("no RSA PRIVATE KEY or PRIVATE KEY PEM block found")
}

func parseRSAPublicKeyPEM(raw []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in actor public key")
	}
	if block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("PEM type %q, expected PUBLIC KEY", block.Type)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is %T, want RSA", pub)
	}
	return rsaPub, nil
}
