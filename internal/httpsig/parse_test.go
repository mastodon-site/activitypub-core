package httpsig

import (
	"testing"
)

func TestParseSignatureHeader(t *testing.T) {
	raw := `keyId="https://ex.test/u/a#main",algorithm="rsa-sha256",headers="(request-target) host date",signature="YWJj"`
	m, err := ParseSignatureHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m["keyid"] != "https://ex.test/u/a#main" {
		t.Fatalf("keyid: got %q", m["keyid"])
	}
	if m["algorithm"] != "rsa-sha256" {
		t.Fatalf("algorithm: %q", m["algorithm"])
	}
	if m["signature"] != "YWJj" {
		t.Fatalf("signature: %q", m["signature"])
	}
	if m["headers"] != "(request-target) host date" {
		t.Fatalf("headers: %q", m["headers"])
	}
}

func TestParseSignatureHeader_rejectsEmpty(t *testing.T) {
	if _, err := ParseSignatureHeader(""); err == nil {
		t.Fatal("expected error")
	}
}
