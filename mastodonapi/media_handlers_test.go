package mastodonapi

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

// Reproduces mastodon_features_integration_test multipart upload (CreateFormFile → octet-stream).
func TestReadMediaUpload_createFormFileYieldsPNGFromFilename(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("fakepng")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	s := &Server{}
	parsed, err := s.readMediaUpload(req, 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.FileBody) == 0 {
		t.Fatal("empty body")
	}
	if got := effectiveUploadContentType(parsed.FileContentType, parsed.FileName); got != "image/png" {
		t.Fatalf("effective MIME %q (fileCT=%q filename=%q)", got, parsed.FileContentType, parsed.FileName)
	}
}

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
