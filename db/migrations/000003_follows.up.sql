CREATE TABLE IF NOT EXISTS follows (
    id                BIGSERIAL PRIMARY KEY,
    follower_actor_id BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    followee_actor_id BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    state             TEXT NOT NULL DEFAULT 'pending',
    follow_activity_id TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (follower_actor_id, followee_actor_id),
    UNIQUE (follow_activity_id)
);

CREATE INDEX IF NOT EXISTS follows_followee_idx ON follows (followee_actor_id);
CREATE INDEX IF NOT EXISTS follows_follower_idx ON follows (follower_actor_id);
