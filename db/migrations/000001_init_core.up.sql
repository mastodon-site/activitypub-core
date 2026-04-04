-- Core federation + job queue (forward-only). Expand in later migrations.

CREATE TABLE IF NOT EXISTS actors (
    id           BIGSERIAL PRIMARY KEY,
    username     TEXT NOT NULL,
    domain       TEXT NOT NULL DEFAULT '',
    actor_url    TEXT NOT NULL UNIQUE,
    inbox_url    TEXT NOT NULL,
    outbox_url   TEXT NOT NULL,
    public_key_pem TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (username, domain)
);

CREATE TABLE IF NOT EXISTS objects (
    id         BIGSERIAL PRIMARY KEY,
    object_url TEXT NOT NULL UNIQUE,
    actor_id   BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    raw_json   JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS activities (
    id          BIGSERIAL PRIMARY KEY,
    activity_id TEXT NOT NULL UNIQUE,
    actor_id    BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    raw_json    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS deliveries (
    id           BIGSERIAL PRIMARY KEY,
    activity_db_id BIGINT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    inbox_url    TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    attempts     INT NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS queue_jobs (
    id              BIGSERIAL PRIMARY KEY,
    job_type        TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    idempotency_key TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    run_after       TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at       TIMESTAMPTZ,
    attempts        INT NOT NULL DEFAULT 0,
    finished_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS queue_jobs_pending_run_after_idx
    ON queue_jobs (run_after, id)
    WHERE status = 'pending';

CREATE UNIQUE INDEX IF NOT EXISTS queue_jobs_idempotency_key_uq
    ON queue_jobs (idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
