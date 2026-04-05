-- App-level OAuth tokens (client_credentials) have no user actor.
ALTER TABLE oauth_access_tokens
    ALTER COLUMN actor_id DROP NOT NULL;
