package aphttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithCORS_disabledWhenEmpty(t *testing.T) {
	called := false
	h := WithCORS(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://a.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !called {
		t.Fatal("inner handler not called")
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("code %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unexpected CORS header when disabled")
	}
}

func TestWithCORS_wildcardAPIReflectsOrigin(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := WithCORS([]string{"*"}, inner)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apps", nil)
	req.Header.Set("Origin", "https://client.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://client.example" {
		t.Fatalf("Allow-Origin %q, want request Origin echoed for /api", got)
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentials required for credentialed browser clients when Origin is set")
	}
}

func TestWithCORS_wildcardNonAPIUsesStar(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := WithCORS([]string{"*"}, inner)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/webfinger", nil)
	req.Header.Set("Origin", "https://client.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin %q", got)
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must not be set with * origin")
	}
}

func TestWithCORS_preflightOptions(t *testing.T) {
	called := false
	h := WithCORS([]string{"http://localhost:3000"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/apps", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if called {
		t.Fatal("inner handler should not run for OPTIONS preflight")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("code %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("headers %#v", rr.Header())
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected credentials for explicit origin")
	}
}

func TestWithCORS_errorResponseStillHasAllowOrigin(t *testing.T) {
	h := WithCORS([]string{"*"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Origin", "https://x.test")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("browser must see CORS header on error responses")
	}
}
