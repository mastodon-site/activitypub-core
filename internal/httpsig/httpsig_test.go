package httpsig

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

func TestVerifyRequest_acceptsHs2019Algorithm(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{}`)
	req, err := NewSignedPost("https://x.test/inbox", body, "https://k.test/key#main", priv)
	if err != nil {
		t.Fatal(err)
	}
	sig := req.Header.Get("Signature")
	sig = strings.Replace(sig, `algorithm="rsa-sha256"`, `algorithm="hs2019"`, 1)
	req.Header.Set("Signature", sig)
	if err := VerifyRequest(req, body, &priv.PublicKey); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRequest_rejectsWrongPublicKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"x":1}`)
	req, err := NewSignedPost("https://host.test/inbox", body, "https://k/key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRequest(req, body, &other.PublicKey); err == nil {
		t.Fatal("expected verify failure with wrong key")
	}
}

func TestVerifyRequest_rejectsTamperedDateHeader(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{}`)
	req, err := NewSignedPost("https://h/inbox", body, "https://k#main", priv)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Date", time.Now().UTC().Add(-10*time.Minute).Format(http.TimeFormat))
	if err := VerifyRequest(req, body, &priv.PublicKey); err == nil {
		t.Fatal("expected failure when Date no longer matches signed value")
	}
}

func TestVerifyRequest_rejectsClockSkew(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{}`)
	u, err := url.Parse("https://skew.test/inbox")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.URL = u
	req.Header.Set("Host", u.Host)
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Digest", DigestHeader(body))
	req.Header.Set("Date", time.Now().UTC().Add(8*time.Minute).Format(http.TimeFormat))
	signing, err := BuildSigningString(req, HeaderOrder)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Signature", fmt.Sprintf(`keyId="https://k#main",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		strings.Join(HeaderOrder, " "), base64.StdEncoding.EncodeToString(sig)))
	if err := VerifyRequest(req, body, &priv.PublicKey); err == nil {
		t.Fatal("expected Date outside allowed skew")
	}
}

func TestVerifyRequest_rejectsMissingDate(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{}`)
	req, err := NewSignedPost("https://h/inbox", body, "https://k#main", priv)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Del("Date")
	if err := VerifyRequest(req, body, &priv.PublicKey); err == nil {
		t.Fatal("expected missing date failure")
	}
}

func TestVerifyRequest_rejectsUnsupportedAlgorithm(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{}`)
	req, err := NewSignedPost("https://h/inbox", body, "https://k#main", priv)
	if err != nil {
		t.Fatal(err)
	}
	sig := req.Header.Get("Signature")
	sig = strings.Replace(sig, `algorithm="rsa-sha256"`, `algorithm="ed25519"`, 1)
	req.Header.Set("Signature", sig)
	if err := VerifyRequest(req, body, &priv.PublicKey); err == nil {
		t.Fatal("expected algorithm failure")
	}
}

func TestBuildSigningString_requestTargetWithQuery(t *testing.T) {
	u, _ := url.Parse("https://example.com/sharedInbox?foo=1")
	req := &http.Request{Method: http.MethodPost, URL: u, Header: http.Header{}}
	req.Header.Set("Host", "example.com")
	s, err := BuildSigningString(req, []string{"(request-target)"})
	if err != nil {
		t.Fatal(err)
	}
	want := "post /sharedInbox?foo=1"
	if s != "(request-target): "+want {
		t.Fatalf("got %q", s)
	}
}
