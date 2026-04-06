package mastodonapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

const quotedStatusActivityKey = "_quotedStatusActivityId"

// mastodonStatusPresentation augments a Create-backed status with counts, viewer flags, and optional quote.
func (s *Server) mastodonStatusPresentation(ctx context.Context, row store.ActivityRow, viewerActorID int64) (map[string]any, bool) {
	if row.DeletedAt != nil {
		return nil, false
	}
	m, ok := s.mastodonStatusFromCreateRow(ctx, row)
	if !ok {
		return nil, false
	}
	if s.Pool == nil {
		return m, true
	}
	noteIRI, _ := m["uri"].(string)
	noteIRI = strings.TrimSpace(noteIRI)
	if noteIRI != "" {
		nLike, err := store.CountFederatedLikesOnObjectURL(ctx, s.Pool, noteIRI)
		if err == nil {
			m["favourites_count"] = nLike
		}
		nAnn, err := store.CountFederatedAnnouncesOnObjectURL(ctx, s.Pool, noteIRI)
		if err == nil {
			m["reblogs_count"] = nAnn
		}
		if viewerActorID > 0 {
			liked, _ := store.ActorHasLikedObject(ctx, s.Pool, viewerActorID, noteIRI)
			m["favourited"] = liked
			boosted, _ := store.ActorHasAnnouncedObject(ctx, s.Pool, viewerActorID, noteIRI)
			m["reblogged"] = boosted
			sid, err := strconv.ParseInt(fmt.Sprint(m["id"]), 10, 64)
			if err == nil {
				bm, _ := store.StatusBookmarked(ctx, s.Pool, viewerActorID, sid)
				m["bookmarked"] = bm
			}
		}
	}
	s.attachQuotedStatus(ctx, row, m, viewerActorID)
	return m, true
}

func (s *Server) attachQuotedStatus(ctx context.Context, row store.ActivityRow, m map[string]any, viewerActorID int64) {
	var act map[string]any
	if err := json.Unmarshal(row.RawJSON, &act); err != nil {
		return
	}
	note, ok := act["object"].(map[string]any)
	if !ok {
		return
	}
	rawQ, ok := note[quotedStatusActivityKey]
	if !ok || rawQ == nil {
		return
	}
	var qid int64
	switch v := rawQ.(type) {
	case float64:
		qid = int64(v)
	case json.Number:
		n, _ := v.Int64()
		qid = n
	default:
		return
	}
	if qid < 1 {
		return
	}
	qrow, err := store.GetActivityByID(ctx, s.Pool, qid)
	if err != nil || qrow.DeletedAt != nil {
		return
	}
	qst, ok := s.mastodonStatusPresentation(ctx, *qrow, 0)
	if ok {
		m["quoted_status"] = qst
	}
}
