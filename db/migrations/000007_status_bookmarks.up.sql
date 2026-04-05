-- Local bookmarks (Mastodon client UI; not federated as Bookmarks in ActivityPub).
CREATE TABLE IF NOT EXISTS status_bookmarks (
    id                    BIGSERIAL PRIMARY KEY,
    actor_id              BIGINT NOT NULL REFERENCES actors (id) ON DELETE CASCADE,
    status_activity_id    BIGINT NOT NULL REFERENCES activities (id) ON DELETE CASCADE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (actor_id, status_activity_id)
);

CREATE INDEX IF NOT EXISTS status_bookmarks_actor_created_idx ON status_bookmarks (actor_id, created_at DESC);
