package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MastodonFilter models a keyword filter for timeline/context views.
type MastodonFilter struct {
	ID                   int64
	OwnerActorID         int64
	Phrase               string
	WholeWord            bool
	Irreversible         bool
	ExpiresAt            *time.Time
	ContextHome          bool
	ContextNotifications bool
	ContextPublic        bool
	ContextThread        bool
	ContextAccount       bool
}

// InsertMastodonFilter creates a filter; contexts default to home+public+thread when all false.
func InsertMastodonFilter(ctx context.Context, pool *pgxpool.Pool, ownerActorID int64, phrase string, wholeWord, irreversible bool, expiresAt *time.Time, ch, cn, cp, ct, ca bool) (int64, error) {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return 0, fmt.Errorf("phrase required")
	}
	if !ch && !cn && !cp && !ct && !ca {
		ch, cp, ct = true, true, true
	}
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO mastodon_filters (
			owner_actor_id, phrase, whole_word, irreversible, expires_at,
			context_home, context_notifications, context_public, context_thread, context_account
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id
	`, ownerActorID, phrase, wholeWord, irreversible, expiresAt, ch, cn, cp, ct, ca).Scan(&id)
	return id, err
}

// ListMastodonFiltersForActor returns active (non-expired) filters for an owner.
func ListMastodonFiltersForActor(ctx context.Context, pool *pgxpool.Pool, ownerActorID int64) ([]MastodonFilter, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, owner_actor_id, phrase, whole_word, irreversible, expires_at,
			context_home, context_notifications, context_public, context_thread, context_account
		FROM mastodon_filters
		WHERE owner_actor_id = $1
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY id ASC
	`, ownerActorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MastodonFilter
	for rows.Next() {
		var f MastodonFilter
		if err := rows.Scan(&f.ID, &f.OwnerActorID, &f.Phrase, &f.WholeWord, &f.Irreversible, &f.ExpiresAt,
			&f.ContextHome, &f.ContextNotifications, &f.ContextPublic, &f.ContextThread, &f.ContextAccount); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetMastodonFilter returns a filter owned by ownerActorID.
func GetMastodonFilter(ctx context.Context, pool *pgxpool.Pool, filterID, ownerActorID int64) (*MastodonFilter, error) {
	var f MastodonFilter
	err := pool.QueryRow(ctx, `
		SELECT id, owner_actor_id, phrase, whole_word, irreversible, expires_at,
			context_home, context_notifications, context_public, context_thread, context_account
		FROM mastodon_filters WHERE id = $1 AND owner_actor_id = $2
	`, filterID, ownerActorID).Scan(&f.ID, &f.OwnerActorID, &f.Phrase, &f.WholeWord, &f.Irreversible, &f.ExpiresAt,
		&f.ContextHome, &f.ContextNotifications, &f.ContextPublic, &f.ContextThread, &f.ContextAccount)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// UpdateMastodonFilter replaces filter fields.
func UpdateMastodonFilter(ctx context.Context, pool *pgxpool.Pool, filterID, ownerActorID int64, phrase string, wholeWord, irreversible bool, expiresAt *time.Time, ch, cn, cp, ct, ca bool) error {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return fmt.Errorf("phrase required")
	}
	if !ch && !cn && !cp && !ct && !ca {
		ch, cp, ct = true, true, true
	}
	tag, err := pool.Exec(ctx, `
		UPDATE mastodon_filters SET
			phrase = $3, whole_word = $4, irreversible = $5, expires_at = $6,
			context_home = $7, context_notifications = $8, context_public = $9, context_thread = $10, context_account = $11
		WHERE id = $1 AND owner_actor_id = $2
	`, filterID, ownerActorID, phrase, wholeWord, irreversible, expiresAt, ch, cn, cp, ct, ca)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// DeleteMastodonFilter removes a filter owned by ownerActorID.
func DeleteMastodonFilter(ctx context.Context, pool *pgxpool.Pool, filterID, ownerActorID int64) (bool, error) {
	tag, err := pool.Exec(ctx, `DELETE FROM mastodon_filters WHERE id = $1 AND owner_actor_id = $2`, filterID, ownerActorID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
