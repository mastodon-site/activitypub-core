// apd is the ActivityPub HTTP API process (stateless, enqueues work for workers).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/mastodon-site/activitypub-core/aphttp"
	fsblob "github.com/mastodon-site/activitypub-core/blobs/fs"
	s3blob "github.com/mastodon-site/activitypub-core/blobs/s3"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/migrate"
	"github.com/mastodon-site/activitypub-core/observability"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	var st *store.Postgres
	if cfg.DatabaseURL != "" {
		st, err = postgres.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		defer st.Pool.Close()

		migrationsDir := filepath.Join(".", "db", "migrations")
		if mp := os.Getenv("AP_MIGRATIONS_DIR"); mp != "" {
			migrationsDir = mp
		}
		abs, _ := filepath.Abs(migrationsDir)
		if err := migrate.Up(cfg.DatabaseURL, abs); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	// Optional: verify blob backend config (construct but do not serve yet).
	_ = mustBlobStore(ctx, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", aphttp.Health)
	mux.Handle("GET /health/ready", observability.InstrumentHandler("health_ready", http.HandlerFunc(aphttp.Ready(st))))

	ap, err := aphttp.New(cfg)
	if err != nil {
		log.Fatalf("aphttp: %v", err)
	}
	ap.Mount(mux)

	// Metrics on same mux unless separate listener configured separately (TODO split server if AP_METRICS_LISTEN set).
	mux.Handle("GET /metrics", observability.MetricsHandler())

	addr := cfg.HTTPListen
	if cfg.MetricsListen != "" {
		log.Printf("AP_METRICS_LISTEN set to %q — metrics still on main mux; split listener not implemented in bootstrap", cfg.MetricsListen)
	}

	handler := observability.InstrumentHandler("http", mux)

	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("apd listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}

func mustBlobStore(ctx context.Context, cfg *config.Config) any {
	switch cfg.BlobBackend {
	case "filesystem":
		if err := os.MkdirAll(cfg.BlobFSRoot, 0o755); err != nil {
			log.Fatalf("blob fs root: %v", err)
		}
		return fsblob.New(cfg.BlobFSRoot)
	case "s3":
		load := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.BlobS3Region)}
		if cfg.BlobS3Endpoint != "" {
			load = append(load, awsconfig.WithBaseEndpoint(cfg.BlobS3Endpoint))
			if os.Getenv("AP_BLOB_S3_STATIC_CREDS") == "1" {
				load = append(load, awsconfig.WithCredentialsProvider(
					credentials.NewStaticCredentialsProvider(
						os.Getenv("AWS_ACCESS_KEY_ID"),
						os.Getenv("AWS_SECRET_ACCESS_KEY"),
						"",
					)))
			}
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, load...)
		if err != nil {
			log.Fatalf("aws config: %v", err)
		}
		cli := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			if cfg.BlobS3Endpoint != "" {
				o.UsePathStyle = true
			}
		})
		return s3blob.New(cli, cfg.BlobS3Bucket)
	default:
		return nil
	}
}
