package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MastodonList is a user-owned list.
type MastodonList struct {
	ID            int64
	OwnerActorID  int64
	Title         string
	RepliesPolicy string
	Exclusive     bool
}

// InsertMastodonList creates a list owned by ownerActorID.
func InsertMastodonList(ctx context.Context, pool *pgxpool.Pool, ownerActorID int64, title string) (int64, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New list"
	}
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO mastodon_lists (owner_actor_id, title) VALUES ($1, $2) RETURNING id
	`, ownerActorID, title).Scan(&id)
	return id, err
}

// GetMastodonList returns a list if owned by ownerActorID.
func GetMastodonList(ctx context.Context, pool *pgxpool.Pool, listID, ownerActorID int64) (*MastodonList, error) {
	var m MastodonList
	m.RepliesPolicy = "list"
	m.Exclusive = false
	err := pool.QueryRow(ctx, `
		SELECT id, owner_actor_id, title FROM mastodon_lists WHERE id = $1 AND owner_actor_id = $2
	`, listID, ownerActorID).Scan(&m.ID, &m.OwnerActorID, &m.Title)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// UpdateMastodonListTitle renames a list.
func UpdateMastodonListTitle(ctx context.Context, pool *pgxpool.Pool, listID, ownerActorID int64, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title required")
	}
	tag, err := pool.Exec(ctx, `
		UPDATE mastodon_lists SET title = $3 WHERE id = $1 AND owner_actor_id = $2
	`, listID, ownerActorID, title)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// DeleteMastodonList removes a list and its members.
func DeleteMastodonList(ctx context.Context, pool *pgxpool.Pool, listID, ownerActorID int64) (bool, error) {
	tag, err := pool.Exec(ctx, `DELETE FROM mastodon_lists WHERE id = $1 AND owner_actor_id = $2`, listID, ownerActorID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListMastodonLists returns lists owned by ownerActorID (newest first).
func ListMastodonLists(ctx context.Context, pool *pgxpool.Pool, ownerActorID int64) ([]MastodonList, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, owner_actor_id, title FROM mastodon_lists
		WHERE owner_actor_id = $1 ORDER BY id DESC
	`, ownerActorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MastodonList
	for rows.Next() {
		var m MastodonList
		m.RepliesPolicy = "list"
		m.Exclusive = false
		if err := rows.Scan(&m.ID, &m.OwnerActorID, &m.Title); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMastodonListMember adds an account to a list (idempotent).
func AddMastodonListMember(ctx context.Context, pool *pgxpool.Pool, listID, ownerActorID, memberActorID int64) error {
	_, err := GetMastodonList(ctx, pool, listID, ownerActorID)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO mastodon_list_members (list_id, member_actor_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, listID, memberActorID)
	return err
}

// RemoveMastodonListMember removes a member from a list owned by ownerActorID.
func RemoveMastodonListMember(ctx context.Context, pool *pgxpool.Pool, listID, ownerActorID, memberActorID int64) error {
	_, err := GetMastodonList(ctx, pool, listID, ownerActorID)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		DELETE FROM mastodon_list_members m
		USING mastodon_lists l
		WHERE m.list_id = l.id AND m.list_id = $1 AND l.owner_actor_id = $2 AND m.member_actor_id = $3
	`, listID, ownerActorID, memberActorID)
	return err
}

// ListMastodonListMemberActorIDs returns member actor ids for a list owned by ownerActorID.
func ListMastodonListMemberActorIDs(ctx context.Context, pool *pgxpool.Pool, listID, ownerActorID int64) ([]int64, error) {
	_, err := GetMastodonList(ctx, pool, listID, ownerActorID)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		SELECT m.member_actor_id FROM mastodon_list_members m
		INNER JOIN mastodon_lists l ON l.id = m.list_id
		WHERE m.list_id = $1 AND l.owner_actor_id = $2
		ORDER BY m.member_actor_id
	`, listID, ownerActorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
