package aphttp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

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
	username := r.PathValue("username")
	if username == "" || !h.cfg.IsLocalUsername(username) {
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
	activityID, err := jsonStringField(rawFields, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	activityType, err := activityTypeString(rawFields)
	if err != nil {
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

	inboxes, err := resolveDeliveryInboxes(r.Context(), h.fetchClient, rawFields, h.cfg.LocalSharedInboxURL())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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
		tx, err := h.st.Pool.Begin(r.Context())
		if err != nil {
			http.Error(w, "tx begin", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())

		inserted, actDBID, err := store.InsertInboundActivity(r.Context(), tx, localActorID, activityID, activityType, body)
		if err != nil {
			http.Error(w, "persist activity", http.StatusInternalServerError)
			return
		}
		if !inserted {
			if err := tx.Commit(r.Context()); err != nil {
				http.Error(w, "tx commit", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		side, err := recordOutboxFollowEffects(r.Context(), tx, h.cfg, localActorID, activityID, activityType, rawFields)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, inbox := range inboxes {
			payload, err := deliverPayload(inbox)
			if err != nil {
				http.Error(w, "internal", http.StatusInternalServerError)
				return
			}
			job := queue.Job{
				Type:           queue.TypeDeliverActivity,
				Payload:        payload,
				IdempotencyKey: activityID + "|" + inbox,
			}
			if err := sqlQ.EnqueueTx(r.Context(), tx, job); err != nil {
				http.Error(w, "enqueue delivery", http.StatusInternalServerError)
				return
			}
		}
		if side != nil && side.enqueueInboxProcess {
			procPayload, err := json.Marshal(map[string]int64{"activityDbId": actDBID})
			if err != nil {
				http.Error(w, "internal", http.StatusInternalServerError)
				return
			}
			procJob := queue.Job{
				Type:           queue.TypeProcessInboxActivity,
				Payload:        procPayload,
				IdempotencyKey: activityID,
			}
			if err := sqlQ.EnqueueTx(r.Context(), tx, procJob); err != nil {
				http.Error(w, "enqueue inbox process", http.StatusInternalServerError)
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, "tx commit", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	tx, err := h.st.Pool.Begin(r.Context())
	if err != nil {
		http.Error(w, "tx begin", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	inserted, actDBID, err := store.InsertInboundActivity(r.Context(), tx, localActorID, activityID, activityType, body)
	if err != nil {
		http.Error(w, "persist activity", http.StatusInternalServerError)
		return
	}
	if !inserted {
		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, "tx commit", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	side, err := recordOutboxFollowEffects(r.Context(), tx, h.cfg, localActorID, activityID, activityType, rawFields)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "tx commit", http.StatusInternalServerError)
		return
	}

	for _, inbox := range inboxes {
		payload, err := deliverPayload(inbox)
		if err != nil {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		job := queue.Job{
			Type:           queue.TypeDeliverActivity,
			Payload:        payload,
			IdempotencyKey: activityID + "|" + inbox,
		}
		if err := h.q.Enqueue(r.Context(), job); err != nil {
			http.Error(w, "enqueue delivery", http.StatusInternalServerError)
			return
		}
	}
	if side != nil && side.enqueueInboxProcess {
		procPayload, err := json.Marshal(map[string]int64{"activityDbId": actDBID})
		if err != nil {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		procJob := queue.Job{
			Type:           queue.TypeProcessInboxActivity,
			Payload:        procPayload,
			IdempotencyKey: activityID,
		}
		if err := h.q.Enqueue(r.Context(), procJob); err != nil {
			http.Error(w, "enqueue inbox process", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
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
		if _, ok := cfg.LocalUsernameForActorURL(store.CanonicalActorURL(objIRI)); ok {
			return &outboxFollowSideEffect{enqueueInboxProcess: true}, nil
		}
		if err := store.UpsertFollow(ctx, tx, localActorID, followeeID, activityID, store.FollowStatePendingRemote); err != nil {
			return nil, err
		}
		return nil, nil
	case strings.EqualFold(t, "Undo"):
		target, err := as2.ObjectIRI(raw)
		if err != nil {
			return nil, err
		}
		if err := store.DeleteFollowByFollowActivityID(ctx, tx, target); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, nil
	}
}

func normalizedOutboxActivityType(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndex(t, "#"); i >= 0 {
		return t[i+1:]
	}
	return t
}

func resolveFolloweeActorID(ctx context.Context, tx pgx.Tx, cfg *config.Config, objectIRI string) (int64, error) {
	canon := store.CanonicalActorURL(objectIRI)
	if _, ok := cfg.LocalUsernameForActorURL(canon); ok {
		return store.ActorIDByActorURLQ(ctx, tx, canon)
	}
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

func resolveDeliveryInboxes(ctx context.Context, client *http.Client, raw map[string]json.RawMessage, localSharedInbox string) ([]string, error) {
	entries := audienceEntries(raw)
	normLocal := strings.TrimRight(localSharedInbox, "/")
	var resolved []string
	seen := make(map[string]struct{})
	for _, e := range entries {
		if skipAudienceEntry(e) {
			continue
		}
		inbox, err := fetch.InboxURLFromReference(ctx, client, e)
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
