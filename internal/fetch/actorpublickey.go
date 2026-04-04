// Package fetch resolves remote ActivityPub actor documents for HTTP Signature verification.
package fetch

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
)

const actorAccept = "application/activity+json, application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\""

// actorDoc is a minimal shape for publicKey extraction (compact JSON).
type actorDoc struct {
	PublicKey json.RawMessage `json:"publicKey"`
}

type publicKeyObj struct {
	ID           string `json:"id"`
	PublicKeyPem string `json:"publicKeyPem"`
}

// PublicKeyForKeyID downloads the actor URL derived from keyId and returns the matching RSA public key.
func PublicKeyForKeyID(ctx context.Context, client *http.Client, keyID string) (*rsa.PublicKey, error) {
	u, err := url.Parse(keyID)
	if err != nil {
		return nil, fmt.Errorf("keyId URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("keyId must be http(s), got %q", u.Scheme)
	}
	fetch := *u
	fetch.Fragment = ""
	fetchURL := fetch.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", actorAccept)
	req.Header.Set("User-Agent", "activitypub-core/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch actor: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("fetch actor %s: %s — %s", fetchURL, resp.Status, strings.TrimSpace(string(slurp)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var doc actorDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode actor json: %w", err)
	}
	if len(doc.PublicKey) == 0 {
		return nil, fmt.Errorf("actor missing publicKey")
	}
	var single publicKeyObj
	if err := json.Unmarshal(doc.PublicKey, &single); err == nil && single.PublicKeyPem != "" {
		if !keyIDsMatch(single.ID, keyID) {
			return nil, fmt.Errorf("actor publicKey.id %q does not match keyId %q", single.ID, keyID)
		}
		return actorkey.ParsePublicKeyPEM([]byte(single.PublicKeyPem))
	}
	var multi []publicKeyObj
	if err := json.Unmarshal(doc.PublicKey, &multi); err != nil {
		return nil, fmt.Errorf("publicKey shape: %w", err)
	}
	for _, k := range multi {
		if keyIDsMatch(k.ID, keyID) && k.PublicKeyPem != "" {
			return actorkey.ParsePublicKeyPEM([]byte(k.PublicKeyPem))
		}
	}
	return nil, fmt.Errorf("no public key in actor matches keyId %s", keyID)
}

func keyIDsMatch(docID, keyID string) bool {
	if docID == "" {
		return false
	}
	return docID == keyID
}
