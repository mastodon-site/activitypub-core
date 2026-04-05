package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestInboxURLFromReference_inboxPathPassthrough(t *testing.T) {
	strict := &Policy{} // public address only (DOCUMENTATION-NET 192.0.2.0/24)
	got, err := InboxURLFromReference(context.Background(), http.DefaultClient, strict, "https://192.0.2.10/users/x/inbox", nil)
	if err != nil || got != "https://192.0.2.10/users/x/inbox" {
		t.Fatalf("got %q %v", got, err)
	}
}

// Regression (security): passthrough must still run policy (no HTTP yet, but host must be allowed).
func TestInboxURLFromReference_strictPolicyRejectsPrivateInPassthrough(t *testing.T) {
	strict := &Policy{}
	_, err := InboxURLFromReference(context.Background(), http.DefaultClient, strict, "https://10.50.50.50/users/x/inbox", nil)
	if err == nil {
		t.Fatal("expected private address rejection on passthrough")
	}
}

func TestInboxURLFromReference_fetchesActorInbox_policyBranch(t *testing.T) {
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
	got, err := InboxURLFromReference(context.Background(), srv.Client(), LaxPolicyForTests(), actorURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != inboxHref {
		t.Fatalf("got %q want %q", got, inboxHref)
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

func TestInboxURLFromReference_detectsCycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/loop" {
			http.NotFound(w, r)
			return
		}
		host := "http://" + r.Host
		self := host + "/users/loop"
		doc := map[string]any{
			"id":    self,
			"inbox": self,
		}
		b, _ := json.Marshal(doc)
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	actorURL := srv.URL + "/users/loop"
	_, err := InboxURLFromReference(context.Background(), srv.Client(), LaxPolicyForTests(), actorURL, nil)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("got %v", err)
	}
}

func TestInboxURLFromReference_depthMaxActorFetches(t *testing.T) {
	n := maxInboxResolutionSteps + 2 // one more actor hop than allowed
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		paths[i] = "/actor/" + strconv.Itoa(i)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := -1
		for i, p := range paths {
			if r.URL.Path == p {
				idx = i
				break
			}
		}
		if idx < 0 {
			http.NotFound(w, r)
			return
		}
		host := "http://" + r.Host
		if idx == len(paths)-1 {
			doc := map[string]any{
				"id":    host + paths[idx],
				"inbox": host + "/inbox/final",
			}
			b, _ := json.Marshal(doc)
			w.Header().Set("Content-Type", "application/activity+json")
			_, _ = w.Write(b)
			return
		}
		next := host + paths[idx+1]
		doc := map[string]any{
			"id":    host + paths[idx],
			"inbox": next,
		}
		b, _ := json.Marshal(doc)
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	start := srv.URL + paths[0]
	_, err := InboxURLFromReference(context.Background(), srv.Client(), LaxPolicyForTests(), start, nil)
	if err == nil {
		t.Fatal("expected depth error")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("got %v", err)
	}
}

func TestInboxURLFromReference_allowsExactlyMaxActorFetches(t *testing.T) {
	n := maxInboxResolutionSteps
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		paths[i] = "/a/" + strconv.Itoa(i)
	}
	wantInbox := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := -1
		for i, p := range paths {
			if r.URL.Path == p {
				idx = i
				break
			}
		}
		if idx < 0 {
			http.NotFound(w, r)
			return
		}
		host := "http://" + r.Host
		if idx == n-1 {
			wantInbox = host + "/inbox/ok"
			doc := map[string]any{
				"id":    host + paths[idx],
				"inbox": wantInbox,
			}
			b, _ := json.Marshal(doc)
			w.Header().Set("Content-Type", "application/activity+json")
			_, _ = w.Write(b)
			return
		}
		doc := map[string]any{
			"id":    host + paths[idx],
			"inbox": host + paths[idx+1],
		}
		b, _ := json.Marshal(doc)
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	got, err := InboxURLFromReference(context.Background(), srv.Client(), LaxPolicyForTests(), srv.URL+paths[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantInbox {
		t.Fatalf("got %q want %q", got, wantInbox)
	}
}
