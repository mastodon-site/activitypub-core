package mastodonapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthScopesSubset(t *testing.T) {
	t.Parallel()
	if !oauthScopesSubset("read", "read write") {
		t.Fatal("read should be subset of read write")
	}
	if !oauthScopesSubset("", "read") {
		t.Fatal("empty requested should be allowed at validation layer")
	}
	if oauthScopesSubset("admin", "read write") {
		t.Fatal("admin not in app scopes")
	}
	if !oauthScopesSubset("read+write", "read write follow") {
		t.Fatal("plus-separated should normalize like form data")
	}
}

func TestParseOAuthTokenParams_formURLEncoded(t *testing.T) {
	t.Parallel()
	body := "grant_type=authorization_code&code=abc&client_id=cid&client_secret=sec&redirect_uri=https%3A%2F%2Fapp%2Fcb&code_verifier=ver"
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("grant_type") != "authorization_code" || vals.Get("code") != "abc" ||
		vals.Get("client_id") != "cid" || vals.Get("client_secret") != "sec" ||
		vals.Get("redirect_uri") != "https://app/cb" || vals.Get("code_verifier") != "ver" {
		t.Fatalf("%+v", vals)
	}
}

func TestParseOAuthTokenParams_formWithQueryFallback(t *testing.T) {
	t.Parallel()
	body := "grant_type=client_credentials&client_id=cid&client_secret=sec"
	req := httptest.NewRequest(http.MethodPost, "/oauth/token?redirect_uri=https%3A%2F%2Fapp%2Fcb", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("redirect_uri") != "https://app/cb" {
		t.Fatal(vals.Get("redirect_uri"))
	}
}

func TestParseOAuthTokenParams_applicationJSON(t *testing.T) {
	t.Parallel()
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "thecode",
		"client_id":     "cid",
		"client_secret": "shh",
		"redirect_uri":  "https://ivory.example/oauth",
		"code_verifier": "pkce-ver",
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range payload {
		if vals.Get(k) != want {
			t.Fatalf("key %s: got %q want %q", k, vals.Get(k), want)
		}
	}
}

func TestParseOAuthTokenParams_applicationJSONCharset(t *testing.T) {
	t.Parallel()
	raw := `{"grant_type":"authorization_code","code":"x","client_id":"a","client_secret":"b","redirect_uri":"https://r"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("grant_type") != "authorization_code" || vals.Get("code") != "x" {
		t.Fatal(vals)
	}
}

func TestParseOAuthTokenParams_clientSecretBasic(t *testing.T) {
	t.Parallel()
	body := "grant_type=authorization_code&code=c&redirect_uri=https%3A%2F%2Fcb"
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("client-id-1", "client-secret-1")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("client_id") != "client-id-1" || vals.Get("client_secret") != "client-secret-1" {
		t.Fatalf("%+v", vals)
	}
}

func TestParseOAuthTokenParams_bodyDoesNotOverrideBasicAuthWhenSet(t *testing.T) {
	t.Parallel()
	body := url.Values{
		"grant_type":    []string{"authorization_code"},
		"code":          []string{"x"},
		"redirect_uri":  []string{"https://cb"},
		"client_id":     []string{"from-form"},
		"client_secret": []string{"from-form-sec"},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("basic-id", "basic-sec")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("client_id") != "from-form" || vals.Get("client_secret") != "from-form-sec" {
		t.Fatal("form values must win over Basic when both present")
	}
}

func TestParseOAuthTokenParams_basicAuthFillsOnlyMissingSecrets(t *testing.T) {
	t.Parallel()
	body := "grant_type=client_credentials&client_id=cid&redirect_uri=https%3A%2F%2Fr"
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("ignored", "from-basic")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("client_id") != "cid" {
		t.Fatal(vals.Get("client_id"))
	}
	if vals.Get("client_secret") != "from-basic" {
		t.Fatal(vals.Get("client_secret"))
	}
}

func TestParseOAuthTokenParams_invalidJSON(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	_, err := parseOAuthTokenParams(req)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseOAuthTokenParams_formContentTypeWithJSONBody(t *testing.T) {
	t.Parallel()
	// Some native clients POST JSON but declare application/x-www-form-urlencoded.
	raw := `{"grant_type":"authorization_code","code":"c1","client_id":"a","client_secret":"sec","redirect_uri":"https://r"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("grant_type") != "authorization_code" || vals.Get("code") != "c1" {
		t.Fatalf("%+v", vals)
	}
}

func TestParseOAuthTokenParams_sniffJSONWhenContentTypeOmitted(t *testing.T) {
	t.Parallel()
	raw := `{"grant_type":"authorization_code","code":"x","client_id":"a","client_secret":"b","redirect_uri":"https://r"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(raw))
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("grant_type") != "authorization_code" || vals.Get("code") != "x" {
		t.Fatalf("%+v", vals)
	}
}

func TestParseOAuthTokenParams_sniffJSONWithTextPlainContentType(t *testing.T) {
	t.Parallel()
	raw := `{"grant_type":"client_credentials","client_id":"a","client_secret":"b","redirect_uri":"https://r"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(raw))
	req.Header.Set("Content-Type", "text/plain")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("grant_type") != "client_credentials" {
		t.Fatal(vals.Get("grant_type"))
	}
}

func TestParseOAuthTokenParams_UTF8BOMPrefix(t *testing.T) {
	t.Parallel()
	raw := "\ufeff" + `{"grant_type":"authorization_code","code":"z"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("code") != "z" {
		t.Fatal(vals.Get("code"))
	}
}

func TestParseOAuthTokenParams_multipartFormData(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("grant_type", "authorization_code"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("code", "code-multipart"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("client_id", "cid"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("client_secret", "s"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("redirect_uri", "https://cb"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	vals, err := parseOAuthTokenParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("grant_type") != "authorization_code" || vals.Get("code") != "code-multipart" {
		t.Fatalf("%+v", vals)
	}
}
