package aphttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/inboxproc"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/store"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// remoteFollowerFixtureWithInbox serves an actor document that includes inbox (for auto-Accept delivery).
func remoteFollowerFixtureWithInbox(t *testing.T) *actorFixture {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr, err := actorkey.PublicKeyPEMFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		kid := base + "/users/remote#main-key"
		actor := map[string]any{
			"@context": []string{"https://www.w3.org/ns/activitystreams"},
			"id":       base + "/users/remote",
			"type":     "Person",
			"inbox":    base + "/inbox",
			"publicKey": map[string]any{
				"id":           kid,
				"owner":        base + "/users/remote",
				"type":         "Key",
				"publicKeyPem": pemStr,
			},
		}
		j, err := json.Marshal(actor)
		if err != nil {
			t.Error(err)
			http.Error(w, "err", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write(j)
	}))
	t.Cleanup(srv.Close)
	return &actorFixture{
		KeyID:  srv.URL + "/users/remote#main-key",
		Priv:   priv,
		Client: srv.Client(),
	}
}

func localProfileFetchClient(baseURL string) *http.Client {
	base := strings.TrimRight(baseURL, "/")
	return &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		u := req.URL.String()
		if strings.HasPrefix(u, base+"/@") {
			doc := map[string]any{"id": u, "inbox": base + "/inbox"}
			b, err := json.Marshal(doc)
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/activity+json"}},
				Body:       io.NopCloser(bytes.NewReader(b)),
				Request:    req,
			}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
}

func TestIntegration_inboundFollow_autoAcceptsAndEnqueuesAcceptDelivery(t *testing.T) {
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

	fix := remoteFollowerFixtureWithInbox(t)
	rec := &recordingQueue{}
	cfg := &config.Config{
		PublicBaseURL:    "https://integration.test",
		LocalUsernames:   []string{"alice"},
		LocalUsername:    "alice",
		InboxMaxBody:     1 << 20,
		FollowAutoAccept: true,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	h.fetchClient = fix.Client

	aliceProfile := cfg.LocalActorProfileURL("alice")
	remoteActor := strings.TrimSuffix(fix.KeyID, "#main-key")
	followID := "https://remote.test/activities/follow-1"
	body := mustJSON(t, map[string]any{
		"type":   "Follow",
		"id":     followID,
		"actor":  remoteActor,
		"object": aliceProfile,
	})
	req := mustSignedPost(t, "https://integration.test/inbox", body, fix.KeyID, fix.Priv)
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	var actDBID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM activities WHERE activity_id = $1`, followID).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, fix.Client, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}

	remoteID, err := store.ActorIDByActorURL(ctx, pool, remoteActor)
	if err != nil {
		t.Fatal(err)
	}
	aliceID := h.localActorIDs["alice"]
	stFollow, err := store.GetFollowState(ctx, pool, remoteID, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStateAccepted {
		t.Fatalf("follow state %q want %q", stFollow, store.FollowStateAccepted)
	}

	jobs := rec.snapshotJobs()
	var delivers int
	for _, j := range jobs {
		if j.Type == queue.TypeDeliverActivity {
			delivers++
			var p inboxproc.DeliverPayload
			if err := json.Unmarshal(j.Payload, &p); err != nil {
				t.Fatal(err)
			}
			var msg map[string]any
			if err := json.Unmarshal(p.Body, &msg); err != nil {
				t.Fatal(err)
			}
			if msg["type"] != "Accept" {
				t.Fatalf("deliver body type %v", msg["type"])
			}
			if p.SigningUsername != "alice" && p.LocalUsername != "alice" {
				t.Fatalf("signing user missing in %+v", p)
			}
		}
	}
	if delivers != 1 {
		t.Fatalf("deliver jobs %d (all jobs: %d)", delivers, len(jobs))
	}
}

func TestIntegration_inboundFollow_noAutoAccept_staysPending(t *testing.T) {
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

	fix := remoteFollowerFixtureWithInbox(t)
	rec := &recordingQueue{}
	cfg := &config.Config{
		PublicBaseURL:    "https://integration.test",
		LocalUsernames:   []string{"alice"},
		LocalUsername:    "alice",
		InboxMaxBody:     1 << 20,
		FollowAutoAccept: false,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	h.fetchClient = fix.Client

	aliceProfile := cfg.LocalActorProfileURL("alice")
	remoteActor := strings.TrimSuffix(fix.KeyID, "#main-key")
	followID := "https://remote.test/activities/follow-2"
	body := mustJSON(t, map[string]any{
		"type":   "Follow",
		"id":     followID,
		"actor":  remoteActor,
		"object": aliceProfile,
	})
	rr := httptest.NewRecorder()
	h.SharedInbox(rr, mustSignedPost(t, "https://integration.test/inbox", body, fix.KeyID, fix.Priv))
	if rr.Code != http.StatusAccepted {
		t.Fatal(rr.Code)
	}
	var actDBID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM activities WHERE activity_id = $1`, followID).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, fix.Client, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}
	remoteID, err := store.ActorIDByActorURL(ctx, pool, remoteActor)
	if err != nil {
		t.Fatal(err)
	}
	aliceID := h.localActorIDs["alice"]
	stFollow, err := store.GetFollowState(ctx, pool, remoteID, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStatePending {
		t.Fatalf("follow state %q", stFollow)
	}
	for _, j := range rec.snapshotJobs() {
		if j.Type == queue.TypeDeliverActivity {
			t.Fatalf("unexpected deliver job")
		}
	}
}

func TestIntegration_outboundFollow_localFollowee_enqueuesProcessInbox_andAccepts(t *testing.T) {
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

	const secret = "0123456789abcdef0123456789abcdef"
	rec := &recordingQueue{}
	cfg := &config.Config{
		PublicBaseURL:    "https://integration.test",
		LocalUsernames:   []string{"alice", "bob"},
		LocalUsername:    "alice",
		OutboxPostSecret: secret,
		InboxMaxBody:     1 << 20,
		FollowAutoAccept: true,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	h.fetchClient = localProfileFetchClient(cfg.PublicBaseURL)

	followID := "https://integration.test/activities/alice-follows-bob"
	body := mustJSON(t, map[string]any{
		"type":   "Follow",
		"id":     followID,
		"actor":  cfg.LocalActorProfileURL("alice"),
		"object": cfg.LocalActorProfileURL("bob"),
		"to":     cfg.LocalActorProfileURL("bob"),
	})
	th := testMounted(h)
	req := httptest.NewRequest(http.MethodPost, "https://integration.test/@alice/outbox", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	var sawProc bool
	for _, j := range rec.snapshotJobs() {
		if j.Type == queue.TypeProcessInboxActivity {
			sawProc = true
			break
		}
	}
	if !sawProc {
		t.Fatalf("expected process_inbox job, jobs: %+v", rec.snapshotJobs())
	}

	var actDBID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM activities WHERE activity_id = $1`, followID).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, h.fetchClient, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}

	aliceID := h.localActorIDs["alice"]
	bobID := h.localActorIDs["bob"]
	stFollow, err := store.GetFollowState(ctx, pool, aliceID, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStateAccepted {
		t.Fatalf("follow state %q want accepted", stFollow)
	}
}

// Local-local outbound follow where object uses /users/{name} alias (compatibility with common non-canonical IRIs).
func TestIntegration_outboundFollow_localFollowee_usersPathObject_works(t *testing.T) {
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

	secret := "0123456789abcdef0123456789abcdef"
	rec := &recordingQueue{}
	cfg := &config.Config{
		PublicBaseURL:    "https://integration.test",
		LocalUsernames:   []string{"alice", "bob"},
		LocalUsername:    "alice",
		OutboxPostSecret: secret,
		InboxMaxBody:     1 << 20,
		FollowAutoAccept: true,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	h.fetchClient = localProfileFetchClient(cfg.PublicBaseURL)

	followID := "https://integration.test/activities/alice-follows-bob-users"
	body := mustJSON(t, map[string]any{
		"type":   "Follow",
		"id":     followID,
		"actor":  cfg.LocalActorProfileURL("alice"),
		"object": "https://integration.test/users/bob",
		"to":     cfg.LocalActorProfileURL("bob"),
	})
	th := testMounted(h)
	req := httptest.NewRequest(http.MethodPost, "https://integration.test/@alice/outbox", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	var actDBID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM activities WHERE activity_id = $1`, followID).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, h.fetchClient, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}

	aliceID := h.localActorIDs["alice"]
	bobID := h.localActorIDs["bob"]
	stFollow, err := store.GetFollowState(ctx, pool, aliceID, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStateAccepted {
		t.Fatalf("follow state %q want accepted", stFollow)
	}
}

func TestIntegration_outboundFollow_remoteRecordsPendingRemote_andDelivers(t *testing.T) {
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

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := "http://" + r.Host
		doc := map[string]any{
			"id":    host + "/users/remote",
			"inbox": host + "/inbox/remote",
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
		LocalUsernames:   []string{"alice"},
		LocalUsername:    "alice",
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

	followID := "https://integration.test/activities/alice-follows-remote"
	body := mustJSON(t, map[string]any{
		"type":   "Follow",
		"id":     followID,
		"actor":  cfg.LocalActorProfileURL("alice"),
		"to":     remote.URL + "/users/remote",
		"object": remote.URL + "/users/remote",
	})
	th := testMounted(h)
	req := httptest.NewRequest(http.MethodPost, "https://integration.test/@alice/outbox", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}

	aliceID := h.localActorIDs["alice"]
	remoteID, err := store.ActorIDByActorURL(ctx, pool, remote.URL+"/users/remote")
	if err != nil {
		t.Fatal(err)
	}
	stFollow, err := store.GetFollowState(ctx, pool, aliceID, remoteID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStatePendingRemote {
		t.Fatalf("follow state %q want pending_remote", stFollow)
	}
	var delivers int
	for _, j := range rec.snapshotJobs() {
		if j.Type == queue.TypeDeliverActivity {
			delivers++
		}
	}
	if delivers != 1 {
		t.Fatalf("deliver jobs %d", delivers)
	}
	for _, j := range rec.snapshotJobs() {
		if j.Type == queue.TypeProcessInboxActivity {
			t.Fatal("unexpected process_inbox for remote followee")
		}
	}
}

func TestIntegration_inboundAccept_updatesPendingRemote(t *testing.T) {
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

	cfg := &config.Config{
		PublicBaseURL:  "https://integration.test",
		LocalUsernames: []string{"alice"},
		LocalUsername:  "alice",
	}
	rec := &recordingQueue{}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	aliceID := h.localActorIDs["alice"]

	remoteURL := "https://remoteacc.test/users/r"
	rid, err := store.EnsureRemoteActor(ctx, pool, remoteURL, "-----BEGIN PUBLIC KEY-----\nmw\n-----END PUBLIC KEY-----\n")
	if err != nil {
		t.Fatal(err)
	}
	followAct := "https://remoteacc.test/act/follow-x"
	if err := store.UpsertFollow(ctx, pool, rid, aliceID, followAct, store.FollowStatePendingRemote); err != nil {
		t.Fatal(err)
	}

	acceptAct := "https://remoteacc.test/act/accept-1"
	raw, err := json.Marshal(map[string]any{
		"type":   "Accept",
		"id":     acceptAct,
		"actor":  remoteURL,
		"object": followAct,
	})
	if err != nil {
		t.Fatal(err)
	}
	var actDBID int64
	if err := pool.QueryRow(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Accept',$3) RETURNING id`,
		acceptAct, rid, raw).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, http.DefaultClient, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}
	stFollow, err := store.GetFollowState(ctx, pool, rid, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStateAccepted {
		t.Fatalf("state %q want accepted", stFollow)
	}
}

func TestIntegration_inboundUndo_deletesFollow(t *testing.T) {
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

	cfg := &config.Config{
		PublicBaseURL:  "https://integration.test",
		LocalUsernames: []string{"alice"},
		LocalUsername:  "alice",
	}
	rec := &recordingQueue{}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	aliceID := h.localActorIDs["alice"]

	remoteURL := "https://remoteacc.test/users/r2"
	rid, err := store.EnsureRemoteActor(ctx, pool, remoteURL, "-----BEGIN PUBLIC KEY-----\nmw\n-----END PUBLIC KEY-----\n")
	if err != nil {
		t.Fatal(err)
	}
	followAct := "https://remoteacc.test/act/follow-y"
	if err := store.UpsertFollow(ctx, pool, rid, aliceID, followAct, store.FollowStateAccepted); err != nil {
		t.Fatal(err)
	}

	undoAct := "https://remoteacc.test/act/undo-1"
	undoRaw, err := json.Marshal(map[string]any{
		"type":   "Undo",
		"id":     undoAct,
		"actor":  remoteURL,
		"object": followAct,
	})
	if err != nil {
		t.Fatal(err)
	}
	var undoDBID int64
	if err := pool.QueryRow(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Undo',$3) RETURNING id`,
		undoAct, rid, undoRaw).Scan(&undoDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, http.DefaultClient, undoDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetFollowState(ctx, pool, rid, aliceID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected follow removed, err=%v", err)
	}
}

func TestIntegration_postOutbox_undoDeletesPendingRemote(t *testing.T) {
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

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := "http://" + r.Host
		doc := map[string]any{
			"id":    host + "/users/remote",
			"inbox": host + "/inbox/remote",
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
		LocalUsernames:   []string{"alice"},
		LocalUsername:    "alice",
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
	th := testMounted(h)

	followID := "https://integration.test/activities/alice-follows-remote-uf"
	followBody := mustJSON(t, map[string]any{
		"type":   "Follow",
		"id":     followID,
		"actor":  cfg.LocalActorProfileURL("alice"),
		"to":     remote.URL + "/users/remote",
		"object": remote.URL + "/users/remote",
	})
	req := httptest.NewRequest(http.MethodPost, "https://integration.test/@alice/outbox", bytes.NewReader(followBody))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/activity+json")
	rr := httptest.NewRecorder()
	th.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("follow: status %d %s", rr.Code, rr.Body.String())
	}

	aliceID := h.localActorIDs["alice"]
	remoteID, err := store.ActorIDByActorURL(ctx, pool, remote.URL+"/users/remote")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetFollowState(ctx, pool, aliceID, remoteID); err != nil {
		t.Fatal(err)
	}

	undoID := "https://integration.test/activities/undo-that-follow"
	undoBody := mustJSON(t, map[string]any{
		"type":   "Undo",
		"id":     undoID,
		"actor":  cfg.LocalActorProfileURL("alice"),
		"object": followID,
	})
	req2 := httptest.NewRequest(http.MethodPost, "https://integration.test/@alice/outbox", bytes.NewReader(undoBody))
	req2.Header.Set("Authorization", "Bearer "+secret)
	req2.Header.Set("Content-Type", "application/activity+json")
	rr2 := httptest.NewRecorder()
	th.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("undo: status %d %s", rr2.Code, rr2.Body.String())
	}

	if _, err := store.GetFollowState(ctx, pool, aliceID, remoteID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected follow removed after undo, err=%v", err)
	}
}

// Regression (security): another remote actor must not Accept a Follow they did not send.
func TestSecurity_inboundAccept_wrongActorCannotAcceptOthersFollow(t *testing.T) {
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

	cfg := &config.Config{
		PublicBaseURL:  "https://integration.test",
		LocalUsernames: []string{"alice"},
		LocalUsername:  "alice",
	}
	rec := &recordingQueue{}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	aliceID := h.localActorIDs["alice"]

	victimURL := "https://victim-reg.test/users/v"
	victimRID, err := store.EnsureRemoteActor(ctx, pool, victimURL, "pem-v")
	if err != nil {
		t.Fatal(err)
	}
	attackerURL := "https://attacker-reg.test/users/a"
	attackerRID, err := store.EnsureRemoteActor(ctx, pool, attackerURL, "pem-a")
	if err != nil {
		t.Fatal(err)
	}
	followAct := "https://victim-reg.test/activities/follow-reg-1"
	if err := store.UpsertFollow(ctx, pool, victimRID, aliceID, followAct, store.FollowStatePendingRemote); err != nil {
		t.Fatal(err)
	}

	acceptAct := "https://attacker-reg.test/activities/accept-evil"
	raw, err := json.Marshal(map[string]any{
		"type":   "Accept",
		"id":     acceptAct,
		"actor":  attackerURL,
		"object": followAct,
	})
	if err != nil {
		t.Fatal(err)
	}
	var actDBID int64
	if err := pool.QueryRow(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Accept',$3) RETURNING id`,
		acceptAct, attackerRID, raw).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, http.DefaultClient, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}
	stFollow, err := store.GetFollowState(ctx, pool, victimRID, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStatePendingRemote {
		t.Fatalf("wrong Accept flipped follow to %q", stFollow)
	}
}

// Regression (security): another remote actor must not Reject someone else's Follow.
func TestSecurity_inboundReject_wrongActorCannotRejectOthersFollow(t *testing.T) {
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

	cfg := &config.Config{
		PublicBaseURL:  "https://integration.test",
		LocalUsernames: []string{"alice"},
		LocalUsername:  "alice",
	}
	rec := &recordingQueue{}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	aliceID := h.localActorIDs["alice"]

	victimURL := "https://victim-rej.test/users/v"
	victimRID, err := store.EnsureRemoteActor(ctx, pool, victimURL, "pem-v")
	if err != nil {
		t.Fatal(err)
	}
	attackerURL := "https://attacker-rej.test/users/a"
	attackerRID, err := store.EnsureRemoteActor(ctx, pool, attackerURL, "pem-a")
	if err != nil {
		t.Fatal(err)
	}
	followAct := "https://victim-rej.test/activities/follow-rej-1"
	if err := store.UpsertFollow(ctx, pool, victimRID, aliceID, followAct, store.FollowStatePendingRemote); err != nil {
		t.Fatal(err)
	}

	rejectAct := "https://attacker-rej.test/activities/reject-evil"
	raw, err := json.Marshal(map[string]any{
		"type":   "Reject",
		"id":     rejectAct,
		"actor":  attackerURL,
		"object": followAct,
	})
	if err != nil {
		t.Fatal(err)
	}
	var actDBID int64
	if err := pool.QueryRow(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Reject',$3) RETURNING id`,
		rejectAct, attackerRID, raw).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, http.DefaultClient, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}
	stFollow, err := store.GetFollowState(ctx, pool, victimRID, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStatePendingRemote {
		t.Fatalf("wrong Reject flipped follow to %q", stFollow)
	}
}

// Regression (security): another remote actor must not Undo someone else's Follow.
func TestSecurity_inboundUndo_wrongActorCannotUndoOthersFollow(t *testing.T) {
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

	cfg := &config.Config{
		PublicBaseURL:  "https://integration.test",
		LocalUsernames: []string{"alice"},
		LocalUsername:  "alice",
	}
	rec := &recordingQueue{}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: rec})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	aliceID := h.localActorIDs["alice"]

	victimURL := "https://victim-undo.test/users/v"
	victimRID, err := store.EnsureRemoteActor(ctx, pool, victimURL, "pem-v")
	if err != nil {
		t.Fatal(err)
	}
	attackerURL := "https://attacker-undo.test/users/a"
	attackerRID, err := store.EnsureRemoteActor(ctx, pool, attackerURL, "pem-a")
	if err != nil {
		t.Fatal(err)
	}
	followAct := "https://victim-undo.test/activities/follow-undo-1"
	if err := store.UpsertFollow(ctx, pool, victimRID, aliceID, followAct, store.FollowStateAccepted); err != nil {
		t.Fatal(err)
	}

	undoAct := "https://attacker-undo.test/activities/undo-evil"
	raw, err := json.Marshal(map[string]any{
		"type":   "Undo",
		"id":     undoAct,
		"actor":  attackerURL,
		"object": followAct,
	})
	if err != nil {
		t.Fatal(err)
	}
	var actDBID int64
	if err := pool.QueryRow(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Undo',$3) RETURNING id`,
		undoAct, attackerRID, raw).Scan(&actDBID); err != nil {
		t.Fatal(err)
	}
	if err := inboxproc.ProcessInboxActivity(ctx, pool, rec, cfg, http.DefaultClient, actDBID, fetch.TestingPolicy()); err != nil {
		t.Fatal(err)
	}
	stFollow, err := store.GetFollowState(ctx, pool, victimRID, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	if stFollow != store.FollowStateAccepted {
		t.Fatalf("wrong Undo removed or changed follow (state %q)", stFollow)
	}
}
