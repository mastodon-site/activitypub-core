package aphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PublishLocalActivityBytes validates and enqueues a locally-originated Activity (same pipeline as POST /@user/outbox).
// The JSON must carry id, type, actor (this user), and addressing so resolveDeliveryInboxes can run.
func (h *Handler) PublishLocalActivityBytes(ctx context.Context, username string, body []byte) error {
	if h.cfg.PublicBaseURL == "" || h.st == nil || h.q == nil || len(h.localActorIDs) == 0 {
		return fmt.Errorf("server not configured")
	}
	localActorID, ok := h.localActorIDs[username]
	if !ok || localActorID == 0 {
		return fmt.Errorf("unknown local user %q", username)
	}
	rawFields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(body, &rawFields); err != nil {
		return fmt.Errorf("invalid activity json: %w", err)
	}
	if _, err := jsonStringField(rawFields, "id"); err != nil {
		return err
	}
	if _, err := activityTypeString(rawFields); err != nil {
		return err
	}
	actorIRI, err := actorIRIFromActivity(rawFields)
	if err != nil {
		return err
	}
	localProfile := h.cfg.LocalActorProfileURL(username)
	if strings.TrimRight(actorIRI, "/") != strings.TrimRight(localProfile, "/") {
		return fmt.Errorf("activity actor must be the local user")
	}
	if err := appendFollowerCCForPublicPosts(ctx, h.st.Pool, localActorID, rawFields); err != nil {
		return err
	}
	body, err = json.Marshal(rawFields)
	if err != nil {
		return err
	}
	inboxes, err := resolveDeliveryInboxes(ctx, h.fetchClient, h.fetchPolicy, h.cfg, rawFields, h.cfg.LocalSharedInboxURL())
	if err != nil {
		return fmt.Errorf("delivery: %w", err)
	}
	return h.persistOutboxAndEnqueue(ctx, username, localActorID, body, rawFields, inboxes)
}
