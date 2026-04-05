package inboxproc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/store"
)

func testPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	dsn := testDSN(t)
	if err := migrate.Up(dsn, migDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	truncate(t, pool)
	return ctx, pool
}

func insertProcess(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, actorDBID int64, activityID, colType string, body map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ins, actPK, err := store.InsertInboundActivity(ctx, pool, actorDBID, activityID, colType, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !ins {
		t.Fatal("duplicate activity insert")
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, actPK, nil); err != nil {
		t.Fatalf("process: %v", err)
	}
}

func insertProcessExpectErr(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, actorDBID int64, activityID, colType string, body map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ins, actPK, err := store.InsertInboundActivity(ctx, pool, actorDBID, activityID, colType, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !ins {
		t.Fatal("duplicate activity insert")
	}
	err = ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, actPK, nil)
	if err == nil {
		t.Fatal("expected error from ProcessInboxActivity")
	}
}

func TestIntegration_Create_toPublicOnly(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://pub.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id":           "https://remote.test/o/p1",
		"type":         "Note",
		"attributedTo": "https://remote.test/users/a",
	}
	noteRaw, _ := json.Marshal(note)
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/cp", "Create", map[string]any{
		"id":     "https://remote.test/act/cp",
		"type":   "Create",
		"actor":  "https://remote.test/users/a",
		"to":     []string{"https://www.w3.org/ns/activitystreams#Public"},
		"object": json.RawMessage(noteRaw),
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1`, "https://remote.test/o/p1").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("objects %d", cnt)
	}
}

func TestIntegration_Create_objectIRIOnly_noObjectRow(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://iri.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/cref", "Create", map[string]any{
		"id":     "https://remote.test/act/cref",
		"type":   "Create",
		"actor":  "https://remote.test/users/a",
		"to":     []string{cfg.LocalSharedInboxURL()},
		"object": "https://elsewhere.test/obj/only-iri",
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected no object materialized, got %d", cnt)
	}
}

func TestIntegration_Create_typeFromActivityRowWhenJSONHasNoType(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://rowt.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id":           "https://remote.test/o/rowt",
		"type":         "Note",
		"attributedTo": "https://remote.test/users/a",
	}
	noteRaw, _ := json.Marshal(note)
	raw, err := json.Marshal(map[string]any{
		"id":     "https://remote.test/act/rowt",
		"actor":  "https://remote.test/users/a",
		"to":     []string{cfg.LocalSharedInboxURL()},
		"object": json.RawMessage(noteRaw),
	})
	if err != nil {
		t.Fatal(err)
	}
	ins, pk, err := store.InsertInboundActivity(ctx, pool, rid, "https://remote.test/act/rowt", "Create", raw)
	if err != nil || !ins {
		t.Fatal(err)
	}
	if err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, pk, nil); err != nil {
		t.Fatal(err)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1`, "https://remote.test/o/rowt").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("objects %d", cnt)
	}
}

func TestIntegration_Update_afterCreate(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://upd.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id":           "https://remote.test/o/up1",
		"type":         "Note",
		"attributedTo": "https://remote.test/users/a",
		"content":      "v1",
	}
	noteRaw, _ := json.Marshal(note)
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/c1", "Create", map[string]any{
		"id": "https://remote.test/act/c1", "type": "Create", "actor": "https://remote.test/users/a",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": json.RawMessage(noteRaw),
	})
	note2 := map[string]any{
		"id": "https://remote.test/o/up1", "type": "Note",
		"attributedTo": "https://remote.test/users/a", "content": "v2",
	}
	n2, _ := json.Marshal(note2)
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/u1", "Update", map[string]any{
		"id": "https://remote.test/act/u1", "type": "Update", "actor": "https://remote.test/users/a",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": json.RawMessage(n2),
	})
	var content string
	if err := pool.QueryRow(ctx, `SELECT raw_json->>'content' FROM objects WHERE object_url = $1`, "https://remote.test/o/up1").Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "v2" {
		t.Fatalf("content %q", content)
	}
}

func TestIntegration_Delete_tombstoneObject(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://tomb.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id": "https://remote.test/o/t1", "type": "Note",
		"attributedTo": "https://remote.test/users/a",
	}
	noteRaw, _ := json.Marshal(note)
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/tc", "Create", map[string]any{
		"id": "https://remote.test/act/tc", "type": "Create", "actor": "https://remote.test/users/a",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": json.RawMessage(noteRaw),
	})
	tomb := map[string]any{"type": "Tombstone", "id": "https://remote.test/o/t1"}
	tb, _ := json.Marshal(tomb)
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/td", "Delete", map[string]any{
		"id": "https://remote.test/act/td", "type": "Delete", "actor": "https://remote.test/users/a",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": json.RawMessage(tb),
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1 AND deleted_at IS NULL`, "https://remote.test/o/t1").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("want deleted, got %d visible", cnt)
	}
}

func TestIntegration_Delete_unknownObject_noError(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://unk.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/d0", "Delete", map[string]any{
		"id": "https://remote.test/act/d0", "type": "Delete", "actor": "https://remote.test/users/a",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": "https://nope.test/o/missing",
	})
}

func TestIntegration_Delete_wrongOwner(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://own.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	ally, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}
	foe, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/foe", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id": "https://remote.test/o/own", "type": "Note",
		"attributedTo": "https://remote.test/users/ally",
	}
	noteRaw, _ := json.Marshal(note)
	insertProcess(t, ctx, pool, cfg, ally, "https://remote.test/act/co", "Create", map[string]any{
		"id": "https://remote.test/act/co", "type": "Create", "actor": "https://remote.test/users/ally",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": json.RawMessage(noteRaw),
	})
	insertProcessExpectErr(t, ctx, pool, cfg, foe, "https://remote.test/act/dbad", "Delete", map[string]any{
		"id": "https://remote.test/act/dbad", "type": "Delete", "actor": "https://remote.test/users/foe",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": "https://remote.test/o/own",
	})
}

func TestIntegration_Announce_undoEmbedded(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://ann.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/an1", "Announce", map[string]any{
		"id": "https://remote.test/act/an1", "type": "Announce", "actor": "https://remote.test/users/a",
		"to": []string{cfg.LocalSharedInboxURL()}, "object": "https://remote.test/o/ext",
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM federated_announces`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("announces %d", cnt)
	}
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/uan", "Undo", map[string]any{
		"id": "https://remote.test/act/uan", "type": "Undo", "actor": "https://remote.test/users/a",
		"object": map[string]any{
			"type": "Announce", "id": "https://remote.test/act/an1",
		},
	})
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM federated_announces`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("after undo %d", cnt)
	}
}

func TestIntegration_Like_objectAsEmbeddedMap(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://lk.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/lk", "Like", map[string]any{
		"id": "https://remote.test/act/lk", "type": "Like", "actor": "https://remote.test/users/a",
		"to":     []string{cfg.LocalSharedInboxURL()},
		"object": map[string]any{"id": "https://remote.test/o/liked"},
	})
	var ou string
	if err := pool.QueryRow(ctx, `SELECT object_url FROM federated_likes WHERE like_activity_id = $1`, "https://remote.test/act/lk").Scan(&ou); err != nil {
		t.Fatal(err)
	}
	if ou != "https://remote.test/o/liked" {
		t.Fatalf("object_url %q", ou)
	}
}

func TestIntegration_UndoFollow_bareIRI(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://uf.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	bob, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "k")
	if err != nil {
		t.Fatal(err)
	}
	ally, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}
	followIRI := "https://remote.test/act/f1"
	if err := store.UpsertFollow(ctx, pool, ally, bob, followIRI, store.FollowStatePending); err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, ally, "https://remote.test/act/uf", "Undo", map[string]any{
		"id": "https://remote.test/act/uf", "type": "Undo", "actor": "https://remote.test/users/ally",
		"object": followIRI,
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM follows`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("follow rows %d", cnt)
	}
}

func TestIntegration_Noops_flagAndUnknownType(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://noop.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"Flag", "FancyVendorAction"} {
		actID := "https://remote.test/act/noop-" + typ
		insertProcess(t, ctx, pool, cfg, rid, actID, typ, map[string]any{
			"id": actID, "type": typ, "actor": "https://remote.test/users/a",
		})
	}
}

func TestIntegration_ProcessInboxActivity_invalidPrimaryKey(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://badpk.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	err := ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, 0, nil)
	if err == nil {
		t.Fatal("expected error for missing activity row")
	}
}

func TestIntegration_Undo_missingObject_errors(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://umo.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"id":"https://remote.test/act/x","type":"Undo","actor":"https://remote.test/users/a"}`)
	ins, pk, err := store.InsertInboundActivity(ctx, pool, rid, "https://remote.test/act/x", "Undo", raw)
	if err != nil || !ins {
		t.Fatal(err)
	}
	err = ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, pk, nil)
	if err == nil {
		t.Fatal("expected error for undo without object")
	}
}

func TestIntegration_Accept_and_Reject_followState(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://acc.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	bobID, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "k")
	if err != nil {
		t.Fatal(err)
	}
	allyID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}
	followIRI := "https://remote.test/act/facc"
	if err := store.UpsertFollow(ctx, pool, allyID, bobID, followIRI, store.FollowStatePending); err != nil {
		t.Fatal(err)
	}
	bobProfile := cfg.LocalActorProfileURL("bob")
	insertProcess(t, ctx, pool, cfg, bobID, "https://acc.test/act/accept1", "Accept", map[string]any{
		"id": "https://acc.test/act/accept1", "type": "Accept", "actor": bobProfile, "object": followIRI,
	})
	st, err := store.GetFollowState(ctx, pool, allyID, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if st != store.FollowStateAccepted {
		t.Fatalf("state %q", st)
	}
	if err := store.UpsertFollow(ctx, pool, allyID, bobID, "https://remote.test/act/frej", store.FollowStatePending); err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, bobID, "https://acc.test/act/rej1", "Reject", map[string]any{
		"id": "https://acc.test/act/rej1", "type": "Reject", "actor": bobProfile, "object": "https://remote.test/act/frej",
	})
	st, err = store.GetFollowState(ctx, pool, allyID, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if st != store.FollowStateRejected {
		t.Fatalf("state %q", st)
	}
}

func TestIntegration_Accept_wrongActorRejected(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://wacc.test", LocalUsernames: []string{"bob", "carol"}, LocalUsername: "bob"}
	bobID, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "k")
	if err != nil {
		t.Fatal(err)
	}
	carolID, err := store.EnsureLocalActor(ctx, pool, cfg, "carol", "k")
	if err != nil {
		t.Fatal(err)
	}
	allyID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}
	followIRI := "https://remote.test/act/fw"
	if err := store.UpsertFollow(ctx, pool, allyID, bobID, followIRI, store.FollowStatePending); err != nil {
		t.Fatal(err)
	}
	carolProfile := cfg.LocalActorProfileURL("carol")
	insertProcessExpectErr(t, ctx, pool, cfg, carolID, "https://wacc.test/badacc", "Accept", map[string]any{
		"id": "https://wacc.test/badacc", "type": "Accept", "actor": carolProfile, "object": followIRI,
	})
	st, err := store.GetFollowState(ctx, pool, allyID, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if st != store.FollowStatePending {
		t.Fatalf("state should stay pending, got %q", st)
	}
}

func TestIntegration_Undo_nestedUnknownType_noError(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://unku.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "u", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/ux", "Undo", map[string]any{
		"id": "https://remote.test/act/ux", "type": "Undo", "actor": "https://remote.test/users/a",
		"object": map[string]any{"type": "Listen", "id": "https://remote.test/act/z"},
	})
}

func TestIntegration_ProcessInboxActivity_activityMissingAllTypes_errors(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://mt.test", LocalUsernames: []string{"u"}, LocalUsername: "u"}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	// Valid JSON for Postgres, but no type field in document and empty DB type column → dispatch cannot classify.
	_, err = pool.Exec(ctx, `
		INSERT INTO activities (activity_id, actor_id, type, raw_json)
		VALUES ($1, $2, '', '{"id":"https://remote.test/act/notype","actor":"https://remote.test/users/a"}'::jsonb)
	`, "https://remote.test/act/notype", rid)
	if err != nil {
		t.Fatal(err)
	}
	var pk int64
	if err := pool.QueryRow(ctx, `SELECT id FROM activities WHERE activity_id = $1`, "https://remote.test/act/notype").Scan(&pk); err != nil {
		t.Fatal(err)
	}
	err = ProcessInboxActivity(ctx, pool, discardQueue{}, cfg, nil, pk, nil)
	if err == nil || !strings.Contains(err.Error(), "missing type") {
		t.Fatalf("expected missing type error, got %v", err)
	}
}

func TestIntegration_Block_skippedWhenOnlyRemoteAddressing(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://blkrem.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	blocker, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/blockonly", "k")
	if err != nil {
		t.Fatal(err)
	}
	block := map[string]any{
		"id":     "https://remote.test/act/block-only-remote-to",
		"type":   "Block",
		"actor":  "https://remote.test/users/blockonly",
		"to":     []string{"https://somewhere.else.test/users/nobody"},
		"object": "https://evil.test/users/jerk",
	}
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	ins, bid, err := store.InsertInboundActivity(ctx, pool, blocker, "https://remote.test/act/block-only-remote-to", "Block", raw)
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
	if cnt != 0 {
		t.Fatalf("expected block skipped when not addressed to instance, got %d rows", cnt)
	}
}

func TestIntegration_Block_appliesWhenAddressedToSharedInbox(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://blksh.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	blocker, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/blocker2", "k")
	if err != nil {
		t.Fatal(err)
	}
	block := map[string]any{
		"id":     "https://remote.test/act/block-sh",
		"type":   "Block",
		"actor":  "https://remote.test/users/blocker2",
		"to":     []string{cfg.LocalSharedInboxURL()},
		"object": "https://evil.test/users/jerk2",
	}
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	ins, bid, err := store.InsertInboundActivity(ctx, pool, blocker, "https://remote.test/act/block-sh", "Block", raw)
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
		t.Fatalf("federated_blocks %d", cnt)
	}
}

func TestIntegration_Create_addressedViaMastodonUsersPath(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://mastopath.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "k"); err != nil {
		t.Fatal(err)
	}
	rid, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/a", "pem")
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{
		"id":           "https://remote.test/o/mastopath",
		"type":         "Note",
		"attributedTo": "https://remote.test/users/a",
	}
	noteRaw, _ := json.Marshal(note)
	insertProcess(t, ctx, pool, cfg, rid, "https://remote.test/act/cmasto", "Create", map[string]any{
		"id":     "https://remote.test/act/cmasto",
		"type":   "Create",
		"actor":  "https://remote.test/users/a",
		"to":     []string{"https://mastopath.test/users/bob"},
		"object": json.RawMessage(noteRaw),
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1`, "https://remote.test/o/mastopath").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("objects %d", cnt)
	}
}

func TestIntegration_Create_and_Like_fromLocalActor(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{PublicBaseURL: "https://localact.test", LocalUsernames: []string{"bob"}, LocalUsername: "bob"}
	bobID, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "k")
	if err != nil {
		t.Fatal(err)
	}
	profile := cfg.LocalActorProfileURL("bob")
	note := map[string]any{
		"id":           "https://localact.test/o/bob-note",
		"type":         "Note",
		"attributedTo": profile,
		"content":      "from bob",
	}
	noteRaw, _ := json.Marshal(note)
	insertProcess(t, ctx, pool, cfg, bobID, "https://localact.test/act/c1", "Create", map[string]any{
		"id":     "https://localact.test/act/c1",
		"type":   "Create",
		"actor":  profile,
		"to":     []string{cfg.LocalSharedInboxURL()},
		"object": json.RawMessage(noteRaw),
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE object_url = $1`, "https://localact.test/o/bob-note").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("create object rows %d", cnt)
	}
	insertProcess(t, ctx, pool, cfg, bobID, "https://localact.test/act/like1", "Like", map[string]any{
		"id":     "https://localact.test/act/like1",
		"type":   "Like",
		"actor":  profile,
		"to":     []string{cfg.LocalSharedInboxURL()},
		"object": "https://localact.test/o/bob-note",
	})
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM federated_likes`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("likes %d", cnt)
	}
}

func TestIntegration_Follow_objectUsesMastodonUsersPath(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{
		PublicBaseURL:    "https://flo.test",
		LocalUsernames:   []string{"bob"},
		LocalUsername:    "bob",
		FollowAutoAccept: false,
	}
	bobID, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "k")
	if err != nil {
		t.Fatal(err)
	}
	allyID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, allyID, "https://remote.test/act/f-users", "Follow", map[string]any{
		"id":     "https://remote.test/act/f-users",
		"type":   "Follow",
		"actor":  "https://remote.test/users/ally",
		"to":     []string{cfg.LocalActorProfileURL("bob")},
		"object": "https://flo.test/users/bob",
	})
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM follows WHERE follower_actor_id = $1 AND followee_actor_id = $2`, allyID, bobID).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("follow rows %d", cnt)
	}
}

func TestIntegration_Follow_usersPathCaseInsensitive(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{
		PublicBaseURL:    "https://fci.test",
		LocalUsernames:   []string{"admin"},
		LocalUsername:    "admin",
		FollowAutoAccept: false,
	}
	if _, err := store.UpsertLocalActor(ctx, pool, cfg, "admin", "k"); err != nil {
		t.Fatal(err)
	}
	allyID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/ally", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, allyID, "https://remote.test/act/f-ci", "Follow", map[string]any{
		"id":     "https://remote.test/act/f-ci",
		"type":   "Follow",
		"actor":  "https://remote.test/users/ally",
		"to":     []string{cfg.LocalActorProfileURL("admin")},
		"object": "https://fci.test/users/Admin",
	})
	var cnt int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM follows f
		JOIN actors fe ON fe.id = f.followee_actor_id
		WHERE f.follower_actor_id = $1 AND fe.username = 'admin'
	`, allyID).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("follow rows %d", cnt)
	}
}

// Bootstrap "admin" exists only in DB; initial config lists only "carol". Per-job augment must still
// resolve /@admin and record the inbound follow.
func TestIntegration_Follow_dbOnlyLocalUserNotInInitialUsernames(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{
		PublicBaseURL:    "https://aug.test",
		LocalUsernames:   []string{"carol"},
		LocalUsername:    "carol",
		FollowAutoAccept: false,
	}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "carol", "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertLocalActor(ctx, pool, cfg, "admin", "k"); err != nil {
		t.Fatal(err)
	}
	allyID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/allyaug", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcess(t, ctx, pool, cfg, allyID, "https://remote.test/act/f-aug", "Follow", map[string]any{
		"id":     "https://remote.test/act/f-aug",
		"type":   "Follow",
		"actor":  "https://remote.test/users/allyaug",
		"to":     []string{"https://aug.test/@admin"},
		"object": "https://aug.test/@admin",
	})
	var cnt int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM follows f
		JOIN actors fe ON fe.id = f.followee_actor_id
		WHERE f.follower_actor_id = $1 AND fe.username = 'admin'
	`, allyID).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("follow rows %d", cnt)
	}
}

func TestIntegration_Follow_objectFollowersCollectionRejected(t *testing.T) {
	ctx, pool := testPool(t)
	cfg := &config.Config{
		PublicBaseURL:  "https://frej.test",
		LocalUsernames: []string{"bob"},
		LocalUsername:  "bob",
	}
	if _, err := store.EnsureLocalActor(ctx, pool, cfg, "bob", "k"); err != nil {
		t.Fatal(err)
	}
	allyID, err := store.EnsureRemoteActor(ctx, pool, "https://remote.test/users/z", "pem")
	if err != nil {
		t.Fatal(err)
	}
	insertProcessExpectErr(t, ctx, pool, cfg, allyID, "https://remote.test/act/bad-follow-coll", "Follow", map[string]any{
		"id":     "https://remote.test/act/bad-follow-coll",
		"type":   "Follow",
		"actor":  "https://remote.test/users/z",
		"object": cfg.LocalActorFollowersURL("bob"),
	})
}
