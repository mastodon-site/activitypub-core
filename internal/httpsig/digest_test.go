package httpsig

import (
	"testing"
)

func TestVerifyDigest(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	good := DigestHeader(body)
	if err := VerifyDigest(good, body); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDigest("", body); err == nil {
		t.Fatal("expected error on empty digest")
	}
	if err := VerifyDigest("MD5=abc", body); err == nil {
		t.Fatal("expected error on non-SHA-256 digest")
	}
	if err := VerifyDigest(good, []byte(`other`)); err == nil {
		t.Fatal("expected error on body mismatch")
	}
	if err := VerifyDigest("SHA-256=!!!!", body); err == nil {
		t.Fatal("expected error on invalid base64")
	}
}
