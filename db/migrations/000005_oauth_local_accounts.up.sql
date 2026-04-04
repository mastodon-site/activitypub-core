-- Local login + Mastodon-compatible OAuth (Ivory, etc.)

CREATE TABLE IF NOT EXISTS local_accounts (
    actor_id        BIGINT PRIMARY KEY REFERENCES actors(id) ON DELETE CASCADE,
    password_bcrypt TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS oauth_applications (
    id                   BIGSERIAL PRIMARY KEY,
    client_id            TEXT        NOT NULL UNIQUE,
    client_secret_hash   BYTEA       NOT NULL,
    redirect_uris        TEXT        NOT NULL,
    client_name          TEXT        NOT NULL DEFAULT '',
    website              TEXT        NOT NULL DEFAULT '',
    scopes               TEXT        NOT NULL DEFAULT 'read write',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
    id                     BIGSERIAL PRIMARY KEY,
    code                   TEXT        NOT NULL UNIQUE,
    application_id          BIGINT      NOT NULL REFERENCES oauth_applications(id) ON DELETE CASCADE,
    actor_id               BIGINT      NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    redirect_uri           TEXT        NOT NULL,
    scopes                 TEXT        NOT NULL,
    code_challenge         TEXT,
    code_challenge_method  TEXT,
    expires_at             TIMESTAMPTZ NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS oauth_authorization_codes_exp_idx ON oauth_authorization_codes (expires_at);

CREATE TABLE IF NOT EXISTS oauth_access_tokens (
    id               BIGSERIAL PRIMARY KEY,
    token_hash       BYTEA       NOT NULL UNIQUE,
    application_id   BIGINT      NOT NULL REFERENCES oauth_applications(id) ON DELETE CASCADE,
    actor_id         BIGINT      NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    scopes           TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS oauth_access_tokens_actor_idx ON oauth_access_tokens (actor_id);
