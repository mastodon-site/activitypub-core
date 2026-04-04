package httpsig

import (
	"net/http"
	"net/url"
	"testing"
)

func TestBuildSigningString_hostFromRequestHost(t *testing.T) {
	u, _ := url.Parse("https://example.com/sub/inbox")
	req := &http.Request{Method: http.MethodPost, URL: u, Header: http.Header{}, Host: "example.com"}
	s, err := BuildSigningString(req, []string{"host"})
	if err != nil {
		t.Fatal(err)
	}
	if s != "host: example.com" {
		t.Fatalf("got %q", s)
	}
}
