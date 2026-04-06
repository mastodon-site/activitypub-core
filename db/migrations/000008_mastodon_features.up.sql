-- Per-actor signing keys, soft-deleted statuses, Mastodon media + lists + filters.

ALTER TABLE actors ADD COLUMN IF NOT EXISTS private_key_pem TEXT;

ALTER TABLE activities ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS mastodon_media (
    id           BIGSERIAL PRIMARY KEY,
    actor_id     BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    blob_key     TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    byte_size    BIGINT NOT NULL DEFAULT 0,
    description  TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS mastodon_media_actor_idx ON mastodon_media (actor_id);

CREATE TABLE IF NOT EXISTS mastodon_lists (
    id             BIGSERIAL PRIMARY KEY,
    owner_actor_id BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    title          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS mastodon_lists_owner_idx ON mastodon_lists (owner_actor_id);

CREATE TABLE IF NOT EXISTS mastodon_list_members (
    list_id         BIGINT NOT NULL REFERENCES mastodon_lists(id) ON DELETE CASCADE,
    member_actor_id BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    PRIMARY KEY (list_id, member_actor_id)
);

CREATE TABLE IF NOT EXISTS mastodon_filters (
    id                    BIGSERIAL PRIMARY KEY,
    owner_actor_id        BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    phrase                TEXT NOT NULL,
    whole_word            BOOLEAN NOT NULL DEFAULT false,
    irreversible          BOOLEAN NOT NULL DEFAULT false,
    expires_at            TIMESTAMPTZ,
    context_home          BOOLEAN NOT NULL DEFAULT true,
    context_notifications BOOLEAN NOT NULL DEFAULT false,
    context_public        BOOLEAN NOT NULL DEFAULT false,
    context_thread        BOOLEAN NOT NULL DEFAULT false,
    context_account       BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS mastodon_filters_owner_idx ON mastodon_filters (owner_actor_id);
