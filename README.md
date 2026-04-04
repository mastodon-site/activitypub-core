# activitypub-core

Go binaries for ActivityPub federation: **`apd`** (HTTP API) and **`apw`** (background worker).

## HTTP routes

### `apd`

Served on **`AP_HTTP_LISTEN`** (default `:8080`). Unless noted, paths are rooted at the server host (no URL prefix).

| Path | Method(s) | Purpose |
|------|-----------|---------|
| `/.well-known/webfinger` | `GET` | WebFinger (`resource=acct:…`) for the configured local user. |
| `/users/{username}` | `GET` | Actor document (JSON-LD / Activity Streams) for the local user; single path segment only. |
| `/outbox/{username}` | `GET` | Outbox as an `OrderedCollection` of activity IRIs (local user only). |
| `/outbox/{username}` | `POST` | Accept a local activity, persist it, enqueue `deliver_activity` to resolved recipient inboxes (`Authorization: Bearer` + `AP_OUTBOX_POST_SECRET`). |
| `/inbox` | `POST` | Shared inbox: verify HTTP Signatures + Digest, persist activity, enqueue `process_inbox_activity` when DB and queue are configured. |
| `/health/live` | `GET` | Liveness (always OK if process is up). |
| `/health/ready` | `GET` | Readiness; pings Postgres when `AP_DATABASE_URL` is set. |
| `/metrics` | `GET` | Prometheus metrics for the API process. |

`AP_METRICS_LISTEN` is reserved for a separate metrics listener; today metrics also appear on the main mux above.

### `apw`

When **`AP_WORKER_METRICS_LISTEN`** is non-empty, a small HTTP server is started on that address:

| Path | Method(s) | Purpose |
|------|-----------|---------|
| `/metrics` | `GET` | Prometheus metrics for the worker process. |

The worker’s main work is queue consumption, not HTTP serving.
