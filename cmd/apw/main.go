// apw consumes background jobs (delivery, media, etc.).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/worker"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/queue/redisqueue"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store"
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

	// Match apd: config env may omit OAuth/bootstrapped users; inbox follow handling uses
	// cfg.LocalUsernameForInboundFollowObject, which requires merged LocalUsernames.
	if pgPool != nil {
		if err := store.AugmentLocalUsernamesFromDB(ctx, pgPool, cfg); err != nil {
			log.Fatalf("augment local usernames: %v", err)
		}
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
	hc := fetch.NewHTTPClient(cfg, 30*time.Second)
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
		if err := worker.ProcessLease(ctx, cfg, pool, q, lease, hc); err != nil {
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
