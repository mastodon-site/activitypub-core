// apadmin is a small CLI for operator tasks (local users, passwords).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/store"
	"github.com/mastodon-site/activitypub-core/store/postgres"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: apadmin create-user -username NAME -password PASS")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "create-user":
		createUserCmd(os.Args[2:])
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func createUserCmd(args []string) {
	fs := flag.NewFlagSet("create-user", flag.ExitOnError)
	username := fs.String("username", "", "local username (no @)")
	password := fs.String("password", "", "login password for Mastodon OAuth")
	_ = fs.Parse(args)
	if *username == "" || *password == "" {
		log.Fatal("create-user requires -username and -password")
	}
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("AP_DATABASE_URL required")
	}
	if cfg.PublicBaseURL == "" {
		log.Fatal("AP_PUBLIC_BASE_URL required")
	}
	st, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Pool.Close()
	priv, err := actorkey.GenerateRSA2048KeyPair()
	if err != nil {
		log.Fatal(err)
	}
	privPEM, err := actorkey.PrivateKeyToPKCS8PEM(priv)
	if err != nil {
		log.Fatal(err)
	}
	pubPEM, err := actorkey.PublicKeyPEMFromPrivate(priv)
	if err != nil {
		log.Fatal(err)
	}
	id, err := store.UpsertLocalActor(ctx, st.Pool, cfg, *username, pubPEM)
	if err != nil {
		log.Fatal(err)
	}
	if err := store.SetActorRSAKeypair(ctx, st.Pool, id, string(privPEM), pubPEM); err != nil {
		log.Fatal(err)
	}
	if err := store.UpsertLocalAccountPassword(ctx, st.Pool, id, *password); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("ok user %q actor_id=%d (per-actor RSA key stored) — restart apd if it is already running so it picks up the new account.\n", *username, id)
}
