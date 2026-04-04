# activitypub-core

Go binaries for ActivityPub federation: **`apd`** (HTTP API), **`apw`** (background worker), and **`apadmin`** (operator CLI).

## HTTP routes

### `apd`

Served on **`AP_HTTP_LISTEN`** (default `:8080`). Unless noted, paths are rooted at the server host (no URL prefix).

| Path | Method(s) | Purpose |
|------|-----------|---------|
| `/.well-known/webfinger` | `GET` | WebFinger (`resource=acct:…`) for local users on this host. |
| `/@{username}` | `GET` | Actor document (`Person`) for a local user. |
| `/@{username}/outbox` | `GET`, `POST` | Outbox collection (`GET`) or post a locally signed activity (`POST`, `Authorization: Bearer` + `AP_OUTBOX_POST_SECRET`). |
| `/@{username}/followers`, `/@{username}/following` | `GET` | Followers / following collections. |
| `/.well-known/actor` | `GET` | Instance actor document (signed fetch / discovery). |
| `/actor` | `GET` | Alias / redirect to the instance actor. |
| `/media` | `POST` | Media upload (when configured). |
| `/media/{key...}` | `GET` | Fetch uploaded blob by key. |
| `/inbox` | `POST` | Shared inbox: verify HTTP Signatures + Digest, persist activity, enqueue `process_inbox_activity` when DB and queue are configured. |
| `/api/v1/instance` | `GET` | Minimal Mastodon instance metadata (Ivory / client probes). |
| `/api/v1/apps` | `POST` | Register OAuth application (Mastodon-style). |
| `/oauth/authorize` | `GET`, `POST` | Browser login; issues authorization `code`. |
| `/oauth/token` | `POST` | Exchange `code` for `access_token` (`grant_type=authorization_code`, PKCE supported). |
| `/api/v1/accounts/verify_credentials` | `GET` | Current account (`Bearer` access token). |
| `/api/v1/statuses` | `POST` | Create a public Note (`Bearer`). |
| `/api/v1/accounts/search` | `GET` | Search by `acct:user@host`, or HTTPS actor URL. |
| `/api/v1/accounts/{id}/follow` | `POST` | Follow target account id from search (`Bearer`). |
| `/api/v1/timelines/home` | `GET` | Empty list (stub; satisfies clients). |
| `/health/live` | `GET` | Liveness (always OK if process is up). |
| `/health/ready` | `GET` | Readiness; pings Postgres when `AP_DATABASE_URL` is set. |
| `/metrics` | `GET` | Prometheus metrics for the API process. |

**Legacy redirects:** `GET /users/{username}` → `/@{username}`; `GET|POST /outbox/{username}` → `/@{username}/outbox`.

`AP_METRICS_LISTEN` is reserved for a separate metrics listener; today metrics also appear on the main mux above.

### Mastodon-compatible API (Ivory)

1. Set **`AP_PUBLIC_BASE_URL`** to the URL clients use to reach `apd` (for Ivory on a phone this should be a **public HTTPS** URL, e.g. via [ngrok](https://ngrok.com/), not only `http://localhost`).
2. Ensure **`AP_DATABASE_URL`**, **`AP_QUEUE_BACKEND=sql`**, and **`AP_ACTOR_PRIVATE_KEY_PATH`** are set; run **`apw`** so deliveries and inbox processing run.
3. Create a local password user: **`apadmin create-user -username alice -password '...'`** (or rely on `AP_LOCAL_USERNAMES` and still set a password row for OAuth login). Restart **`apd`** after adding users so it merges DB actors with config.
4. In Ivory, register the app (the client calls `POST /api/v1/apps`), then sign in via OAuth (browser opens `/oauth/authorize`).

**Federation checklist:** HTTPS on the public URL, worker running, Postgres migrated, queue drained for `deliver_activity`; remote servers may require signed fetches (`AP_SIGN_GET`, `AP_REQUIRE_AUTHORIZED_FETCH` on peers) per their policy.

**Limitations:** one shared **`AP_ACTOR_PRIVATE_KEY_PATH`** is used to sign deliveries for all local users until per-user keys exist; Mastodon API coverage is minimal (no streaming, notifications, or full status read model); `apw` validates signing users against Postgres when a pool is available.

### Docker Compose

From the repo root, generate a key and start the stack:

```bash
openssl genpkey -algorithm RSA -out private.pem -pkeyopt rsa_keygen_bits:2048
docker compose up --build
```

Override **`AP_PUBLIC_BASE_URL`** (and optionally **`AP_LOCAL_USERNAMES`**) when invoking compose, e.g. `AP_PUBLIC_BASE_URL=https://your-host.example docker compose up`.

### Publishing container images (maintainers)

CI only **builds** images for validation. **GHCR publishes** run from [`.github/workflows/release.yml`](.github/workflows/release.yml):

1. Create a **GitHub Release** from a semver tag (`vMAJOR.MINOR.PATCH`). Publishing the release builds and pushes **`ghcr.io/<owner>/activitypub-core-apd`** and **`activitypub-core-apw`** with tags `v*`, `major.minor`, and `latest`.
2. Package visibility must allow the org/registry to pull (default `GITHUB_TOKEN` needs **packages: write** for this repo—already set on the workflow).
3. To rebuild after a fix, re-run the failed jobs on the release workflow, or delete and re-publish the GitHub Release for that tag (avoid reusing a tag for different commits).

### `apw`

When **`AP_WORKER_METRICS_LISTEN`** is non-empty, a small HTTP server is started on that address:

| Path | Method(s) | Purpose |
|------|-----------|---------|
| `/metrics` | `GET` | Prometheus metrics for the worker process. |

The worker’s main work is queue consumption, not HTTP serving.
