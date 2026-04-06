-- Media processing state, per-attachment sensitive flag (Mastodon API parity).

ALTER TABLE mastodon_media
    ADD COLUMN IF NOT EXISTS processing_state TEXT NOT NULL DEFAULT 'complete',
    ADD COLUMN IF NOT EXISTS sensitive BOOLEAN NOT NULL DEFAULT false;
