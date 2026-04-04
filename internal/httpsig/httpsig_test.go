package httpsig

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestSignAndVerify_roundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey
	body := []byte(`{"type":"Create"}`)
	target := "https://example.com/inbox"
	req, err := NewSignedPost(target, body, "https://origin.test/users/a#main-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRequest(req, body, pub); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRequest_rejectsBadDigest(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"a":1}`)
	u, _ := url.Parse("https://example.org/users/x/inbox")
	req := &http.Request{
		Method: http.MethodPost,
		URL:    u,
		Header: http.Header{},
		Host:   u.Host,
	}
	req.Header.Set("Host", u.Host)
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Digest", DigestHeader([]byte(`other`)))
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	if err := SignPost(req, body, "https://origin/users/a#main-key", priv); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Digest", DigestHeader([]byte(`other`)))
	if err := VerifyRequest(req, body, &priv.PublicKey); err == nil {
		t.Fatal("expected digest mismatch")
	}
}
