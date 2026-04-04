CREATE INDEX IF NOT EXISTS activities_actor_outbox_idx ON activities (actor_id, id DESC);
