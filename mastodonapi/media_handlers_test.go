package mastodonapi

import "testing"

func TestEffectiveUploadContentType_multipartOctetStreamUsesFilename(t *testing.T) {
	got := effectiveUploadContentType("application/octet-stream", "photo.PNG")
	if got != "image/png" {
		t.Fatalf("got %q", got)
	}
}

func TestEffectiveUploadContentType_respectsDeclaredMIME(t *testing.T) {
	got := effectiveUploadContentType("image/jpeg", "file.bin")
	if got != "image/jpeg" {
		t.Fatalf("got %q", got)
	}
}

func TestEffectiveUploadContentType_unknownExtKeepsOctetStream(t *testing.T) {
	got := effectiveUploadContentType("application/octet-stream", "data.bin")
	if got != "application/octet-stream" {
		t.Fatalf("got %q", got)
	}
}
