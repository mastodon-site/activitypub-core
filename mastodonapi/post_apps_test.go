package mastodonapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeAppsRegistrationRequest_JSONRedirectURIsArray(t *testing.T) {
	body := `{"client_name":"Dev","redirect_uris":["http://127.0.0.1/cb","http://localhost/x"],"scopes":"read"}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	name, redir, scopes, website, err := decodeAppsRegistrationRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Dev" {
		t.Fatalf("name %q", name)
	}
	if redir != "http://127.0.0.1/cb\nhttp://localhost/x" {
		t.Fatalf("redirect_uris %q", redir)
	}
	if scopes != "read" {
		t.Fatalf("scopes %q", scopes)
	}
	if website != "" {
		t.Fatalf("website %q", website)
	}
}

func TestDecodeAppsRegistrationRequest_JSONRedirectURIsString(t *testing.T) {
	body := `{"client_name":"A","redirect_uris":"http://x.test/\nhttp://y.test/"}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	name, redir, _, _, err := decodeAppsRegistrationRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if name != "A" || strings.TrimSpace(redir) == "" {
		t.Fatalf("got name=%q redir=%q", name, redir)
	}
}

func TestDecodeAppsRegistrationRequest_formURLEncoded(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader("client_name=F&redirect_uris=http%3A%2F%2Flocal%2F&scopes=read"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	name, redir, scopes, _, err := decodeAppsRegistrationRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if name != "F" || redir != "http://local/" || scopes != "read" {
		t.Fatalf("got %q %q %q", name, redir, scopes)
	}
}

func TestDecodeAppsRegistrationRequest_emptyBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(""))
	r.Header.Set("Content-Type", "application/json")
	_, _, _, _, err := decodeAppsRegistrationRequest(r)
	if err == nil {
		t.Fatal("expected error")
	}
}
