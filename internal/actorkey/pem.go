// Package actorkey loads RSA keys used for ActivityPub actors and HTTP Signatures.
package actorkey

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// LoadPrivateKeyFromFile reads a PKCS#1 or PKCS#8 RSA private key PEM.
func LoadPrivateKeyFromFile(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	return ParsePrivateKeyPEM(raw)
}

// ParsePrivateKeyPEM decodes PKCS#1 or PKCS#8 RSA private key material.
func ParsePrivateKeyPEM(raw []byte) (*rsa.PrivateKey, error) {
	var block *pem.Block
	for len(raw) > 0 {
		block, raw = pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("no PEM block in private key")
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

// ParsePublicKeyPEM decodes a PKIX PUBLIC KEY PEM.
func ParsePublicKeyPEM(raw []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in public key")
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

// PublicKeyPEMFromPrivate returns PKIX PEM text for use in actor publicKeyPem.
func PublicKeyPEMFromPrivate(priv *rsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// ActorPublicKeyPEMForConfig returns PKIX PEM for the actor JSON document (public key file optional).
func ActorPublicKeyPEMForConfig(cfg *config.Config) (string, error) {
	if cfg.ActorPrivateKeyPath == "" {
		return "", fmt.Errorf("actor private key path empty")
	}
	priv, err := LoadPrivateKeyFromFile(cfg.ActorPrivateKeyPath)
	if err != nil {
		return "", err
	}
	if cfg.ActorPublicKeyPath != "" {
		pubRaw, err := os.ReadFile(cfg.ActorPublicKeyPath)
		if err != nil {
			return "", fmt.Errorf("read actor public key: %w", err)
		}
		if _, err := ParsePublicKeyPEM(pubRaw); err != nil {
			return "", fmt.Errorf("parse actor public key: %w", err)
		}
		return string(pubRaw), nil
	}
	return PublicKeyPEMFromPrivate(priv)
}
