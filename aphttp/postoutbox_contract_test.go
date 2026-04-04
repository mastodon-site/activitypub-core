package aphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/store"
)

func TestContract_PostOutbox_requiresSecret(t *testing.T) {
	h, err := New(&config.Config{
		PublicBaseURL: "https://x.test",
		LocalUsername: "u",
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)
	req := httptest.NewRequest(http.MethodPost, "https://x.test/@u/outbox", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestContract_PostOutbox_requiresBearer(t *testing.T) {
	h, err := New(&config.Config{
		PublicBaseURL:    "https://x.test",
		LocalUsername:    "u",
		OutboxPostSecret: "0123456789abcdef0123456789abcdef",
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)
	req := httptest.NewRequest(http.MethodPost, "https://x.test/@u/outbox", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer wrongwrongwrongwrongwrongwrong")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestContract_audienceEntries_dedupesAndFlattens(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "to", `"https://a.test/one"`)
	mustRaw(t, raw, "cc", `[{"id":"https://a.test/one"},"https://b.test/two"]`)
	got := audienceEntries(raw)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestContract_skipAudienceEntry_public(t *testing.T) {
	for _, s := range []string{"https://www.w3.org/ns/activitystreams#Public", "  ", "HTTPS://EXAMPLE/AP#PUBLIC"} {
		if !skipAudienceEntry(s) {
			t.Fatalf("should skip %q", s)
		}
	}
}

func TestContract_PostOutbox_enqueuesDeliver_integration(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateFederationTables(t, pool)

	var resolvedInbox string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := "http://" + r.Host
		resolvedInbox = host + "/inbox/remote"
		doc := map[string]any{
			"id":    host + "/users/remote",
			"inbox": resolvedInbox,
		}
		b, _ := json.Marshal(doc)
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(b)
	}))
	defer remote.Close()

	const secret = "0123456789abcdef0123456789abcdef"
	rec := &recordingQueue{}
	cfg := &config.Config{
		PublicBaseURL:    "https://integration.test",
		LocalUsername:    "localuser",
		OutboxPostSecret: secret,
		InboxMaxBody:     1 << 20,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	h.fetchClient = remote.Client()

	body, err := json.Marshal(map[string]any{
		"type":   "Create",
		"id":     "https://integration.test/o/note-1",
		"actor":  "https://integration.test/@localuser",
		"to":     remote.URL + "/users/remote",
		"object": map[string]any{"id": "https://integration.test/obj/1", "type": "Note"},
	})
	if err != nil {
		t.Fatal(err)
	}

	th := testMounted(h)
	req := httptest.NewRequest(http.MethodPost, "https://integration.test/@localuser/outbox", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	jobs := rec.snapshotJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs %v", jobs)
	}
	if jobs[0].Type != queue.TypeDeliverActivity {
		t.Fatalf("type %s", jobs[0].Type)
	}
	var payload struct {
		InboxURL string          `json:"inboxUrl"`
		Body     json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(jobs[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.InboxURL != resolvedInbox {
		t.Fatalf("inbox %q want %q", payload.InboxURL, resolvedInbox)
	}
}
