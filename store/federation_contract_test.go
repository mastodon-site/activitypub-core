package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("AP_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("set AP_TEST_DATABASE_URL for store integration contract tests")
	}
	return u
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
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
	t.Fatal("could not find db/migrations")
	return ""
}

func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE TABLE queue_jobs, deliveries, follows, federated_likes, federated_announces, federated_blocks, activities, objects, actors RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestIntegration_EnsureLocalActor_returnsStableID(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateAll(t, pool)

	cfg := &config.Config{PublicBaseURL: "https://actor-contract.test", LocalUsernames: []string{"alpha"}, LocalUsername: "alpha"}
	id1, err := EnsureLocalActor(ctx, pool, cfg, "alpha", "pem-one")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := EnsureLocalActor(ctx, pool, cfg, "alpha", "pem-two")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("ids %d vs %d", id1, id2)
	}
}

func TestIntegration_OutboxPage_respectsLimitCap(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateAll(t, pool)

	cfg := &config.Config{PublicBaseURL: "https://cap.test", LocalUsernames: []string{"u1"}, LocalUsername: "u1"}
	localID, err := EnsureLocalActor(ctx, pool, cfg, "u1", "k")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 250 {
		_, err := pool.Exec(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Create','{}')`,
			fmt.Sprintf("https://cap.test/o/%d", i), localID)
		if err != nil {
			t.Fatal(err)
		}
	}
	total, items, _, err := OutboxPage(ctx, pool, localID, 9999, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 250 {
		t.Fatalf("total %d", total)
	}
	if len(items) != 200 {
		t.Fatalf("capped len %d", len(items))
	}
}

func TestIntegration_InsertInboundActivity_duplicateReturnsNotInserted(t *testing.T) {
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	if err := migrate.Up(dsn, findMigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	truncateAll(t, pool)

	rid, err := EnsureRemoteActor(ctx, pool, "https://remote.test/users/zed", "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----\n")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"id":"https://x/1","type":"Create"}`)
	ins, id1, err := InsertInboundActivity(ctx, pool, rid, "https://x/1", "Create", raw)
	if err != nil || !ins || id1 == 0 {
		t.Fatalf("first insert %v %d %v", ins, id1, err)
	}
	ins2, id2, err := InsertInboundActivity(ctx, pool, rid, "https://x/1", "Create", raw)
	if err != nil {
		t.Fatal(err)
	}
	if ins2 || id2 != 0 {
		t.Fatalf("second insert %v %d", ins2, id2)
	}
}
