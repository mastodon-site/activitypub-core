package httpsig

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HeaderOrder is the default signed header list for ActivityPub POSTs (Mastodon-compatible).
var HeaderOrder = []string{"(request-target)", "host", "date", "digest", "content-type"}

// SignPost sets Date, Digest, Signature on req for body using rsa-sha256.
func SignPost(req *http.Request, body []byte, keyID string, priv *rsa.PrivateKey) error {
	if req.URL == nil {
		return fmt.Errorf("nil request URL")
	}
	u := req.URL
	if u.Host == "" {
		return fmt.Errorf("request URL must include host for signing")
	}
	req.Header.Set("Host", u.Host)
	if strings.TrimSpace(req.Header.Get("Content-Type")) == "" {
		req.Header.Set("Content-Type", "application/activity+json")
	}
	req.Header.Set("Digest", DigestHeader(body))
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	signing, err := BuildSigningString(req, HeaderOrder)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		return err
	}
	val := fmt.Sprintf(`keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		keyID,
		strings.Join(HeaderOrder, " "),
		base64.StdEncoding.EncodeToString(sig),
	)
	req.Header.Set("Signature", val)
	return nil
}

// NewSignedPost creates a POST request with Activity+json body and HTTP Signature headers.
func NewSignedPost(targetURL string, body []byte, keyID string, priv *rsa.PrivateKey) (*http.Request, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.URL = u
	req.Host = u.Host
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	if err := SignPost(req, body, keyID, priv); err != nil {
		return nil, err
	}
	return req, nil
}
