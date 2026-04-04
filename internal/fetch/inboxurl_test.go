package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInboxURLFromReference_inboxPathPassthrough(t *testing.T) {
	got, err := InboxURLFromReference(context.Background(), http.DefaultClient, "https://example.test/users/x/inbox")
	if err != nil || got != "https://example.test/users/x/inbox" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestInboxURLFromReference_fetchesActorInbox(t *testing.T) {
	inboxHref := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/bob" {
			inboxHref = "http://" + r.Host + "/inbox/bob"
			doc := map[string]any{
				"id":    "http://" + r.Host + "/users/bob",
				"inbox": inboxHref,
			}
			b, _ := json.Marshal(doc)
			w.Header().Set("Content-Type", "application/activity+json")
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	actorURL := srv.URL + "/users/bob"
	got, err := InboxURLFromReference(context.Background(), srv.Client(), actorURL)
	if err != nil {
		t.Fatal(err)
	}
	if got != inboxHref {
		t.Fatalf("got %q want %q", got, inboxHref)
	}
}
