package httpsig

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const maxClockSkew = 5 * time.Minute

// VerifyRequest checks Digest and rsa-sha256 Signature (Cavage-style) on an inbound request.
// body must be the exact bytes read from r.Body. pub resolves from the Signature keyId (caller supplies).
func VerifyRequest(r *http.Request, body []byte, pub *rsa.PublicKey) error {
	if err := VerifyDigest(r.Header.Get("Digest"), body); err != nil {
		return err
	}
	sigRaw := r.Header.Get("Signature")
	if sigRaw == "" {
		return fmt.Errorf("missing Signature header")
	}
	params, err := ParseSignatureHeader(sigRaw)
	if err != nil {
		return fmt.Errorf("signature header: %w", err)
	}
	alg := params["algorithm"]
	if alg != "rsa-sha256" && alg != "hs2019" {
		return fmt.Errorf("unsupported signature algorithm %q (need rsa-sha256)", alg)
	}
	sigB64 := params["signature"]
	if sigB64 == "" {
		return fmt.Errorf("missing signature value")
	}
	headerList := strings.Fields(params["headers"])
	if len(headerList) == 0 {
		return fmt.Errorf("missing headers list in signature")
	}
	signingString, err := BuildSigningString(r, headerList)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("signature base64: %w", err)
	}
	sum := sha256.Sum256([]byte(signingString))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return fmt.Errorf("signature verify: %w", err)
	}
	if err := verifyDateHeader(r.Header.Get("Date")); err != nil {
		return err
	}
	return nil
}

func verifyDateHeader(date string) error {
	date = strings.TrimSpace(date)
	if date == "" {
		return fmt.Errorf("missing Date header (required with HTTP Signatures)")
	}
	t, err := http.ParseTime(date)
	if err != nil {
		return fmt.Errorf("Date header: %w", err)
	}
	if d := time.Since(t); d > maxClockSkew || d < -maxClockSkew {
		return fmt.Errorf("Date outside allowed skew (%v)", maxClockSkew)
	}
	return nil
}
