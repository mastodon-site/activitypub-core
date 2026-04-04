-- Federated object copies and social edges for inboxproc side effects.

ALTER TABLE objects ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS federated_likes (
    id                 BIGSERIAL PRIMARY KEY,
    actor_id           BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    object_url         TEXT NOT NULL,
    like_activity_id   TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (like_activity_id),
    UNIQUE (actor_id, object_url)
);

CREATE TABLE IF NOT EXISTS federated_announces (
    id                     BIGSERIAL PRIMARY KEY,
    actor_id               BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    object_url             TEXT NOT NULL,
    announce_activity_id   TEXT NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (announce_activity_id),
    UNIQUE (actor_id, object_url)
);

CREATE TABLE IF NOT EXISTS federated_blocks (
    id                  BIGSERIAL PRIMARY KEY,
    blocker_actor_id  BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    blocked_actor_url TEXT NOT NULL,
    block_activity_id TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (block_activity_id),
    UNIQUE (blocker_actor_id, blocked_actor_url)
);

CREATE INDEX IF NOT EXISTS federated_blocks_blocker_idx ON federated_blocks (blocker_actor_id);
CREATE INDEX IF NOT EXISTS federated_likes_object_idx ON federated_likes (object_url);
CREATE INDEX IF NOT EXISTS federated_announces_object_idx ON federated_announces (object_url);
