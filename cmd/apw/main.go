// apw consumes background jobs (delivery, media, etc.).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
	"github.com/mastodon-site/activitypub-core/internal/inboxproc"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/queue/redisqueue"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

var jobsProcessed = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "activitypub_core",
		Subsystem: "worker",
		Name:      "jobs_total",
		Help:      "Jobs processed by outcome.",
	},
	[]string{"type", "result"},
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.DatabaseURL == "" && cfg.QueueBackend == "sql" {
		log.Fatal("AP_DATABASE_URL required for sql queue backend")
	}

	var pgPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		st, err := postgres.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		pgPool = st.Pool
		defer pgPool.Close()
	}

	var q queue.Backend
	switch cfg.QueueBackend {
	case "sql":
		if pgPool == nil {
			log.Fatal("AP_DATABASE_URL required for sql queue backend")
		}
		migrationsDir := filepath.Join(".", "db", "migrations")
		if mp := os.Getenv("AP_MIGRATIONS_DIR"); mp != "" {
			migrationsDir = mp
		}
		abs, err := filepath.Abs(migrationsDir)
		if err != nil {
			log.Fatalf("migrations path: %v", err)
		}
		if err := migrate.Up(cfg.DatabaseURL, abs); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		q = sqlqueue.New(pgPool)
	case "redis":
		if cfg.RedisURL == "" {
			log.Fatal("AP_REDIS_URL required for redis queue backend")
		}
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			log.Fatalf("redis url: %v", err)
		}
		rdb := redis.NewClient(opt)
		defer rdb.Close()
		q = redisqueue.New(rdb)
	default:
		log.Fatalf("unknown queue backend %q", cfg.QueueBackend)
	}

	go func() {
		addr := os.Getenv("AP_WORKER_METRICS_LISTEN")
		if addr == "" {
			return
		}
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", promhttp.Handler())
		log.Printf("worker metrics on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerConcurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			workerLoop(ctx, id, q, cfg, pgPool, cfg.WorkerPollInterval)
		}(i)
	}
	wg.Wait()
	log.Println("apw shut down")
}

func workerLoop(ctx context.Context, id int, q queue.Backend, cfg *config.Config, pool *pgxpool.Pool, poll time.Duration) {
	log.Printf("worker %d started", id)
	hc := &http.Client{Timeout: 30 * time.Second}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		lease, err := q.Dequeue(ctx)
		if err != nil {
			log.Printf("worker %d dequeue: %v", id, err)
			time.Sleep(poll)
			continue
		}
		if lease == nil {
			time.Sleep(poll)
			continue
		}
		if err := processJob(ctx, cfg, pool, q, lease, hc); err != nil {
			jobsProcessed.WithLabelValues(string(lease.Type), "error").Inc()
			log.Printf("worker %d job %d failed: %v", id, lease.ID, err)
			_ = q.Nack(ctx, lease.ID, true)
			continue
		}
		jobsProcessed.WithLabelValues(string(lease.Type), "ok").Inc()
		if err := q.Ack(ctx, lease.ID); err != nil {
			log.Printf("worker %d ack %d: %v", id, lease.ID, err)
		}
	}
}

func processJob(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, q queue.Backend, lease *queue.Lease, httpClient *http.Client) error {
	switch lease.Type {
	case queue.TypeNoop:
		return nil
	case queue.TypeDeliverActivity:
		return deliverActivity(ctx, cfg, lease.Payload)
	case queue.TypeProcessInboxActivity:
		if pool == nil {
			return fmt.Errorf("process_inbox_activity requires AP_DATABASE_URL (postgres pool)")
		}
		var payload struct {
			ActivityDBID int64 `json:"activityDbId"`
		}
		if err := json.Unmarshal(lease.Payload, &payload); err != nil {
			return fmt.Errorf("process_inbox_activity payload: %w", err)
		}
		if payload.ActivityDBID < 1 {
			return fmt.Errorf("process_inbox_activity: activityDbId required")
		}
		return inboxproc.ProcessInboxActivity(ctx, pool, q, cfg, httpClient, payload.ActivityDBID)
	default:
		log.Printf("unknown job type %q — acknowledging", lease.Type)
		return nil
	}
}

type deliverPayload struct {
	InboxURL        string          `json:"inboxUrl"`
	Body            json.RawMessage `json:"body"`
	LocalUsername   string          `json:"localUsername,omitempty"`
	SigningUsername string          `json:"signingUsername,omitempty"`
}

func signingUsernameForDelivery(p deliverPayload, cfg *config.Config) string {
	if strings.TrimSpace(p.SigningUsername) != "" {
		return strings.TrimSpace(p.SigningUsername)
	}
	if strings.TrimSpace(p.LocalUsername) != "" {
		return strings.TrimSpace(p.LocalUsername)
	}
	return cfg.LocalUsername
}

func deliverActivity(ctx context.Context, cfg *config.Config, raw json.RawMessage) error {
	if cfg.ActorPrivateKeyPath == "" {
		return fmt.Errorf("AP_ACTOR_PRIVATE_KEY_PATH required for deliver_activity")
	}
	if cfg.PublicBaseURL == "" {
		return fmt.Errorf("AP_PUBLIC_BASE_URL required for deliver_activity (keyId)")
	}
	priv, err := actorkey.LoadPrivateKeyFromFile(cfg.ActorPrivateKeyPath)
	if err != nil {
		return err
	}
	var p deliverPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("deliver_activity payload: %w", err)
	}
	if p.InboxURL == "" || len(p.Body) == 0 {
		return fmt.Errorf("deliver_activity: inboxUrl and body required")
	}
	user := signingUsernameForDelivery(p, cfg)
	if user == "" || !cfg.IsLocalUsername(user) {
		return fmt.Errorf("deliver_activity: signing user %q is not a configured local account", user)
	}
	keyID := strings.TrimRight(cfg.PublicBaseURL, "/") + "/users/" + url.PathEscape(user) + "#main-key"
	req, err := httpsig.NewSignedPost(p.InboxURL, p.Body, keyID, priv)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("delivery POST %s: %s", resp.Status, strings.TrimSpace(string(slurp)))
	}
	return nil
}
