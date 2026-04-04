package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInboxURLFromReference_atPrefixInbox_passThrough(t *testing.T) {
	ctx := context.Background()
	pol := TestingPolicy()
	for _, raw := range []string{
		"https://ex.test/@alice/inbox",
		"https://ex.test/@alice/inbox/",
		"http://localhost:9999/@u/inbox",
	} {
		got, err := InboxURLFromReference(ctx, http.DefaultClient, pol, raw, nil)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		want := strings.TrimRight(raw, "/")
		if got != want {
			t.Fatalf("%q: got %q want %q", raw, got, want)
		}
	}
}

func TestInboxURLFromReference_usersPathInbox_passThrough(t *testing.T) {
	ctx := context.Background()
	pol := TestingPolicy()
	raw := "https://ex.test/users/bob/inbox"
	got, err := InboxURLFromReference(ctx, http.DefaultClient, pol, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("got %q", got)
	}
}

func TestInboxURLFromReference_inboxWithExtraSegment_passThrough(t *testing.T) {
	ctx := context.Background()
	pol := TestingPolicy()
	raw := "https://ex.test/users/bob/inbox/feed"
	got, err := InboxURLFromReference(ctx, http.DefaultClient, pol, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("got %q", got)
	}
}

func TestInboxURLFromReference_sharedInboxRoot_passThrough(t *testing.T) {
	ctx := context.Background()
	pol := TestingPolicy()
	raw := "https://ex.test/inbox"
	got, err := InboxURLFromReference(ctx, http.DefaultClient, pol, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("got %q", got)
	}
}

func TestInboxURLFromReference_actorFetchesInboxField(t *testing.T) {
	ctx := context.Background()
	pol := TestingPolicy()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/carol" {
			http.NotFound(w, r)
			return
		}
		base := "http://" + r.Host
		doc := map[string]any{"inbox": base + "/@carol/inbox"}
		b, _ := json.Marshal(doc)
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)

	client := srv.Client()
	got, err := InboxURLFromReference(ctx, client, pol, srv.URL+"/users/carol", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := srv.URL + "/@carol/inbox"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestInboxURLFromReference_invalidURL(t *testing.T) {
	ctx := context.Background()
	pol := TestingPolicy()
	_, err := InboxURLFromReference(ctx, http.DefaultClient, pol, "://nope", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
