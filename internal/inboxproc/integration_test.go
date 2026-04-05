package inboxproc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/store"
)

type discardQueue struct{}

func (discardQueue) Enqueue(ctx context.Context, job queue.Job) error { return nil }

func (discardQueue) Dequeue(ctx context.Context) (*queue.Lease, error) { return nil, nil }

func (discardQueue) Ack(ctx context.Context, id int64) error { return nil }

func (discardQueue) Nack(ctx context.Context, id int64, requeue bool) error { return nil }

func testDSN(t *testing.T) string {
	t.Helper()
	u := os.Getenv("AP_TEST_DATABASE_URL")
	if u == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AP_TEST_DATABASE_URL is required in CI")
		}
		t.Skip("set AP_TEST_DATABASE_URL for inboxproc integration tests")
	}
	return u
}

func migDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "db", "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				t.Fatal(err)
			}
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("db/migrations not found")
	return ""
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE TABLE queue_jobs, deliveries, follows, federated_likes, federated_announces, federated_blocks, activities, objects, actors RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestIntegration_CreateLikeUndoDelete(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	if err := migrate.Up(dsn, migDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncate(t, pool)

	cfg := &config.Config{
		PublicBaseURL:  "https://inboxproc.test",
		LocalUsernames: []string{"bob"},
		LocalUsername:  "bob",
	}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "pem"); err != nil {
		t.Fatal(err)
	}
	remoteID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}

	note := map[string]any{
		"id":           "https://remote.test/obj/note-one",
		"type":         "Note",
		"attributedTo": "https://remote.test/users/ally",
		"content":      "hello",
	}
	noteRaw, _ := json.Marshal(note)
	actCreate := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       "https://remote.test/act/create-1",
		"type":     "Create",
		"actor":    "https://remote.test/users/ally",
		"to":       []string{cfg.LocalActorProfileURL("bob")},
		"object":   json.RawMessage(noteRaw),
	}
	createRaw, _ := json.Marshal(actCreate)
	ins, actDBID, err := store.InsertInboundActivity(ctx, pool, remoteID, "https://remote.test/act/create-1", "Create", createRaw)
	if err != nil || !ins {
		t.Fatalf("insert create: %v %v", ins, err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, actDBID, nil); err != nil {
		t.Fatal(err)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1 AND deleted_at IS NULL`, "https://remote.test/obj/note-one").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("objects count %d", cnt)
	}

	likeAct := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       "https://remote.test/act/like-1",
		"type":     "Like",
		"actor":    "https://remote.test/users/ally",
		"to":       []string{cfg.LocalActorProfileURL("bob")},
		"object":   "https://remote.test/obj/note-one",
	}
	likeRaw, _ := json.Marshal(likeAct)
	ins, likeDBID, err := store.InsertInboundActivity(ctx, pool, remoteID, "https://remote.test/act/like-1", "Like", likeRaw)
	if err != nil || !ins {
		t.Fatalf("insert like: %v", err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, likeDBID, nil); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM federated_likes`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("likes %d", cnt)
	}

	undoLike := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       "https://remote.test/act/undo-like",
		"type":     "Undo",
		"actor":    "https://remote.test/users/ally",
		"object": map[string]any{
			"type": "Like",
			"id":   "https://remote.test/act/like-1",
		},
	}
	undoRaw, _ := json.Marshal(undoLike)
	ins, undoDBID, err := store.InsertInboundActivity(ctx, pool, remoteID, "https://remote.test/act/undo-like", "Undo", undoRaw)
	if err != nil || !ins {
		t.Fatalf("insert undo: %v", err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, undoDBID, nil); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM federated_likes`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("likes after undo %d", cnt)
	}

	delAct := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       "https://remote.test/act/del-1",
		"type":     "Delete",
		"actor":    "https://remote.test/users/ally",
		"to":       []string{cfg.LocalActorProfileURL("bob")},
		"object":   "https://remote.test/obj/note-one",
	}
	delRaw, _ := json.Marshal(delAct)
	ins, delDBID, err := store.InsertInboundActivity(ctx, pool, remoteID, "https://remote.test/act/del-1", "Delete", delRaw)
	if err != nil || !ins {
		t.Fatalf("insert delete: %v", err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, delDBID, nil); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1 AND deleted_at IS NULL`, "https://remote.test/obj/note-one").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected soft-deleted object, visible rows %d", cnt)
	}
}

func TestIntegration_Create_skippedWhenNotAddressedToInstance(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	if err := migrate.Up(dsn, migDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncate(t, pool)

	cfg := &config.Config{
		PublicBaseURL:  "https://addr.test",
		LocalUsernames: []string{"bob"},
		LocalUsername:  "bob",
	}
	remoteID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/x", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id":           "https://remote.test/obj/orphan",
		"type":         "Note",
		"attributedTo": "https://remote.test/users/x",
	}
	noteRaw, _ := json.Marshal(note)
	act := map[string]any{
		"id":     "https://remote.test/act/c1",
		"type":   "Create",
		"actor":  "https://remote.test/users/x",
		"to":     []string{"https://someone.else.test/users/y"},
		"object": json.RawMessage(noteRaw),
	}
	raw, _ := json.Marshal(act)
	ins, dbid, err := store.InsertInboundActivity(ctx, pool, remoteID, "https://remote.test/act/c1", "Create", raw)
	if err != nil || !ins {
		t.Fatal(err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, dbid, nil); err != nil {
		t.Fatal(err)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1`, "https://remote.test/obj/orphan").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected no object, got %d", cnt)
	}
}

func TestIntegration_BlockAndUndo(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	if err := migrate.Up(dsn, migDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncate(t, pool)

	cfg := &config.Config{PublicBaseURL: "https://blk.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	blocker, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/blocker", "k")
	if err != nil {
		t.Fatal(err)
	}
	block := map[string]any{
		"id":     "https://remote.test/act/block-1",
		"type":   "Block",
		"actor":  "https://remote.test/users/blocker",
		"object": "https://evil.test/users/jerk",
	}
	raw, _ := json.Marshal(block)
	ins, bid, err := store.InsertInboundActivity(ctx, pool, blocker, "https://remote.test/act/block-1", "Block", raw)
	if err != nil || !ins {
		t.Fatal(err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, bid, nil); err != nil {
		t.Fatal(err)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM federated_blocks`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("blocks %d", cnt)
	}
	undo := map[string]any{
		"id":     "https://remote.test/act/ub",
		"type":   "Undo",
		"actor":  "https://remote.test/users/blocker",
		"object": "https://remote.test/act/block-1",
	}
	uraw, _ := json.Marshal(undo)
	ins, uid, err := store.InsertInboundActivity(ctx, pool, blocker, "https://remote.test/act/ub", "Undo", uraw)
	if err != nil || !ins {
		t.Fatal(err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, uid, nil); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM federated_blocks`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("blocks after undo %d", cnt)
	}
}

func TestIntegration_Create_rejectsAttributionMismatch(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	if err := migrate.Up(dsn, migDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncate(t, pool)

	cfg := &config.Config{PublicBaseURL: "https://bad.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	remoteID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id":           "https://remote.test/obj/bad",
		"type":         "Note",
		"attributedTo": "https://someone.else/users/imposter",
	}
	noteRaw, _ := json.Marshal(note)
	act := map[string]any{
		"id":     "https://remote.test/act/bad-c",
		"type":   "Create",
		"actor":  "https://remote.test/users/ally",
		"to":     []string{cfg.LocalSharedInboxURL()},
		"object": json.RawMessage(noteRaw),
	}
	raw, _ := json.Marshal(act)
	ins, dbid, err := store.InsertInboundActivity(ctx, pool, remoteID, "https://remote.test/act/bad-c", "Create", raw)
	if err != nil || !ins {
		t.Fatal(err)
	}
	err = ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, dbid, nil)
	if err == nil {
		t.Fatal("expected attribution error")
	}
}
