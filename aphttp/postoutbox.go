package aphttp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/as2"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store"
)

// PostOutbox accepts a locally-originated Activity (application/activity+json or ld+json),
// stores it for the local actor, and enqueues deliver_activity for each resolved recipient inbox.
// Requires AP_OUTBOX_POST_SECRET and Authorization: Bearer <secret>.
func (h *Handler) PostOutbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.TrimSpace(h.cfg.OutboxPostSecret) == "" {
		http.Error(w, "outbound post not configured", http.StatusServiceUnavailable)
		return
	}
	if !outboxBearerAuthorized(r, h.cfg.OutboxPostSecret) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.cfg.PublicBaseURL == "" || h.st == nil || h.q == nil || len(h.localActorIDs) == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	handle := r.PathValue("handle")
	username, ok := parseAtHandle(handle)
	if !ok || !h.IsLocalActor(username) {
		http.NotFound(w, r)
		return
	}
	localActorID, ok := h.localActorIDs[username]
	if !ok || localActorID == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	ct := r.Header.Get("Content-Type")
	if !isActivityJSONContentType(ct) {
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}
	max := h.inboxMaxBody
	if max <= 0 {
		max = 1 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if len(body) > max {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	rawFields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(body, &rawFields); err != nil {
		http.Error(w, "invalid activity json", http.StatusBadRequest)
		return
	}
	if _, err := jsonStringField(rawFields, "id"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := activityTypeString(rawFields); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actorIRI, err := actorIRIFromActivity(rawFields)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	localProfile := h.cfg.LocalActorProfileURL(username)
	if strings.TrimRight(actorIRI, "/") != strings.TrimRight(localProfile, "/") {
		http.Error(w, "activity actor must be the local user", http.StatusBadRequest)
		return
	}

	if err := appendFollowerCCForPublicPosts(r.Context(), h.st.Pool, localActorID, rawFields); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body, err = json.Marshal(rawFields)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	inboxes, err := resolveDeliveryInboxes(r.Context(), h.fetchClient, h.fetchPolicy, h.cfg, rawFields, h.cfg.LocalSharedInboxURL())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.persistOutboxAndEnqueue(r.Context(), username, localActorID, body, rawFields, inboxes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func appendFollowerCCForPublicPosts(ctx context.Context, pool *pgxpool.Pool, localActorID int64, raw map[string]json.RawMessage) error {
	if pool == nil {
		return nil
	}
	hasPublic := false
	for _, e := range audienceEntries(raw) {
		if skipAudienceEntry(e) {
			hasPublic = true
			break
		}
	}
	if !hasPublic {
		return nil
	}
	urls, err := store.ListAcceptedFollowerActorURLs(ctx, pool, localActorID)
	if err != nil {
		return err
	}
	return mergeStringIRIsIntoJSONField(raw, "cc", urls)
}

func mergeStringIRIsIntoJSONField(raw map[string]json.RawMessage, key string, add []string) error {
	if len(add) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var elems []any
	if existing, ok := raw[key]; ok {
		for _, s := range flattenJSONLDRefs(existing) {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			seen[s] = struct{}{}
			elems = append(elems, s)
		}
	}
	for _, s := range add {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		elems = append(elems, s)
	}
	if len(elems) == 0 {
		return nil
	}
	b, err := json.Marshal(elems)
	if err != nil {
		return err
	}
	raw[key] = b
	return nil
}

func (h *Handler) persistOutboxAndEnqueue(ctx context.Context, username string, localActorID int64, body []byte, rawFields map[string]json.RawMessage, inboxes []string) error {
	activityID, err := jsonStringField(rawFields, "id")
	if err != nil {
		return err
	}
	activityType, err := activityTypeString(rawFields)
	if err != nil {
		return err
	}

	type deliverJobPayload struct {
		InboxURL        string          `json:"inboxUrl"`
		Body            json.RawMessage `json:"body"`
		SigningUsername string          `json:"signingUsername,omitempty"`
		LocalUsername   string          `json:"localUsername,omitempty"`
	}
	deliverPayload := func(inbox string) ([]byte, error) {
		return json.Marshal(deliverJobPayload{
			InboxURL:        inbox,
			Body:            body,
			SigningUsername: username,
			LocalUsername:   username,
		})
	}

	if sqlQ, ok := h.q.(*sqlqueue.SQL); ok {
		tx, err := h.st.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("tx begin: %w", err)
		}
		defer tx.Rollback(ctx)

		inserted, actDBID, err := store.InsertInboundActivity(ctx, tx, localActorID, activityID, activityType, body)
		if err != nil {
			return fmt.Errorf("persist activity: %w", err)
		}
		if !inserted {
			return tx.Commit(ctx)
		}
		side, err := recordOutboxFollowEffects(ctx, tx, h.cfg, localActorID, activityID, activityType, rawFields)
		if err != nil {
			return err
		}
		for _, inbox := range inboxes {
			payload, err := deliverPayload(inbox)
			if err != nil {
				return err
			}
			job := queue.Job{
				Type:           queue.TypeDeliverActivity,
				Payload:        payload,
				IdempotencyKey: activityID + "|" + inbox,
			}
			if err := sqlQ.EnqueueTx(ctx, tx, job); err != nil {
				return fmt.Errorf("enqueue delivery: %w", err)
			}
		}
		if side != nil && side.enqueueInboxProcess {
			procPayload, err := json.Marshal(map[string]int64{"activityDbId": actDBID})
			if err != nil {
				return err
			}
			procJob := queue.Job{
				Type:           queue.TypeProcessInboxActivity,
				Payload:        procPayload,
				IdempotencyKey: activityID,
			}
			if err := sqlQ.EnqueueTx(ctx, tx, procJob); err != nil {
				return fmt.Errorf("enqueue inbox process: %w", err)
			}
		}
		return tx.Commit(ctx)
	}

	tx, err := h.st.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("tx begin: %w", err)
	}
	defer tx.Rollback(ctx)

	inserted, actDBID, err := store.InsertInboundActivity(ctx, tx, localActorID, activityID, activityType, body)
	if err != nil {
		return fmt.Errorf("persist activity: %w", err)
	}
	if !inserted {
		return tx.Commit(ctx)
	}
	side, err := recordOutboxFollowEffects(ctx, tx, h.cfg, localActorID, activityID, activityType, rawFields)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	for _, inbox := range inboxes {
		payload, err := deliverPayload(inbox)
		if err != nil {
			return err
		}
		job := queue.Job{
			Type:           queue.TypeDeliverActivity,
			Payload:        payload,
			IdempotencyKey: activityID + "|" + inbox,
		}
		if err := h.q.Enqueue(ctx, job); err != nil {
			return fmt.Errorf("enqueue delivery: %w", err)
		}
	}
	if side != nil && side.enqueueInboxProcess {
		procPayload, err := json.Marshal(map[string]int64{"activityDbId": actDBID})
		if err != nil {
			return err
		}
		procJob := queue.Job{
			Type:           queue.TypeProcessInboxActivity,
			Payload:        procPayload,
			IdempotencyKey: activityID,
		}
		if err := h.q.Enqueue(ctx, procJob); err != nil {
			return fmt.Errorf("enqueue inbox process: %w", err)
		}
	}
	return nil
}

type outboxFollowSideEffect struct {
	enqueueInboxProcess bool
}

func recordOutboxFollowEffects(
	ctx context.Context,
	tx pgx.Tx,
	cfg *config.Config,
	localActorID int64,
	activityID, activityType string,
	raw map[string]json.RawMessage,
) (*outboxFollowSideEffect, error) {
	t := normalizedOutboxActivityType(activityType)
	switch {
	case strings.EqualFold(t, "Follow"):
		objIRI, err := as2.ObjectIRI(raw)
		if err != nil {
			return nil, err
		}
		followeeID, err := resolveFolloweeActorID(ctx, tx, cfg, objIRI)
		if err != nil {
			return nil, err
		}
		if followeeID == localActorID {
			return nil, fmt.Errorf("cannot follow self")
		}
		if _, ok := cfg.LocalUsernameForInboundFollowObject(store.CanonicalActorURL(objIRI)); ok {
			return &outboxFollowSideEffect{enqueueInboxProcess: true}, nil
		}
		if err := store.UpsertFollow(ctx, tx, localActorID, followeeID, activityID, store.FollowStatePendingRemote); err != nil {
			return nil, err
		}
		return nil, nil
	case strings.EqualFold(t, "Like"):
		objIRI, err := as2.ObjectIRI(raw)
		if err != nil {
			return nil, err
		}
		if err := store.UpsertFederatedLike(ctx, tx, localActorID, store.CanonicalActorURL(objIRI), activityID); err != nil {
			return nil, err
		}
		return nil, nil
	case strings.EqualFold(t, "Announce"):
		objIRI, err := as2.ObjectIRI(raw)
		if err != nil {
			return nil, err
		}
		if err := store.UpsertFederatedAnnounce(ctx, tx, localActorID, store.CanonicalActorURL(objIRI), activityID); err != nil {
			return nil, err
		}
		return nil, nil
	case strings.EqualFold(t, "Undo"):
		if err := recordOutboxUndoEffects(ctx, tx, localActorID, raw); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, nil
	}
}

func recordOutboxUndoEffects(ctx context.Context, tx pgx.Tx, localActorID int64, raw map[string]json.RawMessage) error {
	target, err := undoObjectTargetIRI(raw)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM follows WHERE follow_activity_id = $1 AND follower_actor_id = $2
	`, target, localActorID)
	if err != nil {
		return fmt.Errorf("undo follow: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag, err = tx.Exec(ctx, `
		DELETE FROM federated_likes WHERE like_activity_id = $1 AND actor_id = $2
	`, target, localActorID)
	if err != nil {
		return fmt.Errorf("undo like: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag, err = tx.Exec(ctx, `
		DELETE FROM federated_announces WHERE announce_activity_id = $1 AND actor_id = $2
	`, target, localActorID)
	if err != nil {
		return fmt.Errorf("undo announce: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag, err = tx.Exec(ctx, `
		DELETE FROM federated_blocks WHERE block_activity_id = $1 AND blocker_actor_id = $2
	`, target, localActorID)
	if err != nil {
		return fmt.Errorf("undo block: %w", err)
	}
	return nil
}

func undoObjectTargetIRI(raw map[string]json.RawMessage) (string, error) {
	r, ok := raw["object"]
	if !ok {
		return "", fmt.Errorf("undo missing object")
	}
	var s string
	if err := json.Unmarshal(r, &s); err == nil && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r, &obj); err != nil {
		return "", fmt.Errorf("undo object: %w", err)
	}
	idRaw, ok := obj["id"]
	if !ok {
		return "", fmt.Errorf("undo object missing id")
	}
	if err := json.Unmarshal(idRaw, &s); err != nil || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("undo object invalid id")
	}
	return strings.TrimSpace(s), nil
}

func normalizedOutboxActivityType(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndex(t, "#"); i >= 0 {
		return t[i+1:]
	}
	return t
}

func resolveFolloweeActorID(ctx context.Context, tx pgx.Tx, cfg *config.Config, objectIRI string) (int64, error) {
	if id, _, err := store.ResolveLocalFolloweeFromObjectIRI(ctx, tx, cfg, objectIRI); err == nil {
		return id, nil
	}
	base, err := url.Parse(strings.TrimSpace(cfg.PublicBaseURL))
	if err != nil || base.Hostname() == "" {
		return 0, fmt.Errorf("invalid instance config")
	}
	if obj, err := url.Parse(strings.TrimSpace(objectIRI)); err == nil && obj.Hostname() != "" {
		if strings.EqualFold(obj.Hostname(), base.Hostname()) {
			return 0, fmt.Errorf("follow object is not a local actor")
		}
	}
	canon := store.CanonicalActorURL(objectIRI)
	return store.EnsureRemoteActor(ctx, tx, canon, "(remote-followee)")
}

func outboxBearerAuthorized(r *http.Request, secret string) bool {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	tok := strings.TrimSpace(parts[1])
	if len(tok) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1
}

func audienceEntries(m map[string]json.RawMessage) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, key := range []string{"to", "cc", "bto", "bcc", "audience"} {
		raw, ok := m[key]
		if !ok {
			continue
		}
		for _, ref := range flattenJSONLDRefs(raw) {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if _, dup := seen[ref]; dup {
				continue
			}
			seen[ref] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}

func flattenJSONLDRefs(raw json.RawMessage) []string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return []string{s}
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		var out []string
		for _, el := range arr {
			out = append(out, flattenJSONLDRefs(el)...)
		}
		return out
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.ID != "" {
		return []string{obj.ID}
	}
	return nil
}

func skipAudienceEntry(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "#public") {
		return true
	}
	if strings.Contains(lower, "activitystreams#public") {
		return true
	}
	return false
}

func resolveDeliveryInboxes(ctx context.Context, client *http.Client, policy *fetch.Policy, cfg *config.Config, raw map[string]json.RawMessage, localSharedInbox string) ([]string, error) {
	entries := audienceEntries(raw)
	normLocal := strings.TrimRight(localSharedInbox, "/")
	var resolved []string
	seen := make(map[string]struct{})
	for _, e := range entries {
		if skipAudienceEntry(e) {
			continue
		}
		if cfg != nil && cfg.IsLocalActorFollowersOrFollowingCollectionIRI(e) {
			// Followers/following are Collections; fan-out uses individual actor IRIs merged into cc.
			continue
		}
		inbox, err := fetch.InboxURLFromReference(ctx, client, policy, e, cfg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e, err)
		}
		n := strings.TrimRight(inbox, "/")
		if n == normLocal {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		resolved = append(resolved, n)
	}
	return resolved, nil
}
