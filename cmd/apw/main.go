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

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
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

	var q queue.Backend
	switch cfg.QueueBackend {
	case "sql":
		st, err := postgres.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		defer st.Pool.Close()
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
		q = sqlqueue.New(st.Pool)
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
			workerLoop(ctx, id, q, cfg, cfg.WorkerPollInterval)
		}(i)
	}
	wg.Wait()
	log.Println("apw shut down")
}

func workerLoop(ctx context.Context, id int, q queue.Backend, cfg *config.Config, poll time.Duration) {
	log.Printf("worker %d started", id)
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
		if err := processJob(ctx, cfg, lease); err != nil {
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

func processJob(ctx context.Context, cfg *config.Config, lease *queue.Lease) error {
	switch lease.Type {
	case queue.TypeNoop:
		return nil
	case queue.TypeDeliverActivity:
		return deliverActivity(ctx, cfg, lease.Payload)
	default:
		log.Printf("unknown job type %q — acknowledging", lease.Type)
		return nil
	}
}

type deliverPayload struct {
	InboxURL string          `json:"inboxUrl"`
	Body     json.RawMessage `json:"body"`
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
	keyID := strings.TrimRight(cfg.PublicBaseURL, "/") + "/users/" + url.PathEscape(cfg.LocalUsername) + "#main-key"
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
