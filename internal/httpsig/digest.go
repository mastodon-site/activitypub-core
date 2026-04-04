package httpsig

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const digestPrefix = "SHA-256="

// DigestHeader returns the Digest header value (SHA-256 over body).
func DigestHeader(body []byte) string {
	sum := sha256.Sum256(body)
	return digestPrefix + base64.StdEncoding.EncodeToString(sum[:])
}

// VerifyDigest compares the Digest header to SHA-256(body) using constant-time equality on the digest bytes.
func VerifyDigest(digestHeader string, body []byte) error {
	digestHeader = strings.TrimSpace(digestHeader)
	if digestHeader == "" {
		return fmt.Errorf("missing Digest header")
	}
	if !strings.HasPrefix(digestHeader, digestPrefix) {
		return fmt.Errorf("Digest must use SHA-256")
	}
	want := sha256.Sum256(body)
	gotB64 := strings.TrimPrefix(digestHeader, digestPrefix)
	got, err := base64.StdEncoding.DecodeString(gotB64)
	if err != nil {
		return fmt.Errorf("digest base64: %w", err)
	}
	if len(got) != len(want) {
		return fmt.Errorf("digest length mismatch")
	}
	var diff byte
	for i := range want {
		diff |= want[i] ^ got[i]
	}
	if diff != 0 {
		return fmt.Errorf("digest does not match request body")
	}
	return nil
}
