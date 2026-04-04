package httpsig

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignGet_VerifyGet_roundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://ex.test/foo/bar?x=1", nil)
	req = req.WithContext(context.Background())
	if err := SignGet(req, "https://ex.test/a#main", priv); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGet(req, &priv.PublicKey); err != nil {
		t.Fatal(err)
	}
}
