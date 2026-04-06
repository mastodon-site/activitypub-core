package aphttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/store"
)

func TestIntegration_GETActivityOrObject_authorizedFetch(t *testing.T) {
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
	_, err = pool.Exec(ctx, `TRUNCATE TABLE queue_jobs, deliveries, follows, federated_likes, federated_announces, federated_blocks, activities, objects, actors RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := t.TempDir() + "/actor.pem"
	blk := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(blk), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		PublicBaseURL:               "https://af.test",
		LocalUsernames:              []string{"alice"},
		LocalUsername:               "alice",
		ActorPrivateKeyPath:         keyPath,
		RequireAuthorizedFetch:      true,
		InstanceActorPrivateKeyPath: keyPath,
	}
	st := &store.Postgres{Pool: pool}
	h, err := New(cfg, Deps{Store: st, Queue: nil})
	if err != nil {
		t.Fatal(err)
	}
	applyTestingFetchPolicy(h)
	actorIRI := cfg.InstanceActorIRI()
	h.fetchClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && strings.HasPrefix(strings.TrimRight(req.URL.String(), "/"), strings.TrimRight(actorIRI, "/")) {
			b, err := json.Marshal(instanceActorJSON(cfg, h.instancePublicKeyPEM))
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

	actID := "https://af.test/posts/p1"
	raw := []byte(`{"@context":"https://www.w3.org/ns/activitystreams","id":"` + actID + `","type":"Create","actor":"` + cfg.LocalActorProfileURL("alice") + `"}`)
	aliceDB := h.localActorIDs["alice"]
	if _, err := pool.Exec(ctx, `INSERT INTO activities (activity_id, actor_id, type, raw_json) VALUES ($1,$2,'Create',$3::jsonb)`,
		actID, aliceDB, raw); err != nil {
		t.Fatal(err)
	}
	th := testMounted(h)

	t.Run("unsigned_rejected", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, actID, nil)
		th.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("want 401 got %d", rr.Code)
		}
	})

	t.Run("signed_ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, actID, nil)
		if err := httpsig.SignGet(req, cfg.InstanceActorKeyID(), priv); err != nil {
			t.Fatal(err)
		}
		rr := httptest.NewRecorder()
		th.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200 got %d %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("outbox_unsigned_401", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://af.test/@alice/outbox", nil)
		th.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("want 401 got %d", rr.Code)
		}
	})

	t.Run("followers_unsigned_401", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://af.test/@alice/followers", nil)
		th.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("want 401 got %d", rr.Code)
		}
	})

	t.Run("followers_signed_ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://af.test/@alice/followers", nil)
		if err := httpsig.SignGet(req, cfg.InstanceActorKeyID(), priv); err != nil {
			t.Fatal(err)
		}
		rr := httptest.NewRecorder()
		th.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200 got %d %s", rr.Code, rr.Body.String())
		}
	})
}
