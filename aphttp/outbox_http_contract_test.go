package aphttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestContract_GetOutbox_methodNotAllowed(t *testing.T) {
	h, err := New(&config.Config{PublicBaseURL: "https://o.test", LocalUsername: "u"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://o.test/outbox/u", nil)
	rr := httptest.NewRecorder()
	h.GetOutbox(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rr.Code)
	}
}
