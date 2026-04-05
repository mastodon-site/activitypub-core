package mastodonapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mastodon-site/activitypub-core/store"
)

func timelineLimit(r *http.Request) int {
	l := strings.TrimSpace(r.URL.Query().Get("limit"))
	if l == "" {
		return 20
	}
	n, err := strconv.Atoi(l)
	if err != nil || n < 1 {
		return 20
	}
	if n > 80 {
		return 80
	}
	return n
}

func objectIsNote(obj map[string]any) bool {
	switch t := obj["type"].(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "Note")
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && strings.HasSuffix(strings.TrimSpace(s), "Note") {
				return true
			}
		}
	}
	return false
}

func publishedFromNoteOrCreate(act, note map[string]any) string {
	if s, ok := note["published"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	if s, ok := act["published"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func (s *Server) mastodonStatusFromCreateRow(ctx context.Context, row store.ActivityRow) (map[string]any, bool) {
	var act map[string]any
	if err := json.Unmarshal(row.RawJSON, &act); err != nil {
		return nil, false
	}
	obj, ok := act["object"]
	if !ok {
		return nil, false
	}
	note, ok := obj.(map[string]any)
	if !ok || !objectIsNote(note) {
		return nil, false
	}
	acct, err := s.accountMap(ctx, row.ActorID)
	if err != nil {
		return nil, false
	}
	content, _ := note["content"].(string)
	noteIRI, _ := note["id"].(string)
	return map[string]any{
		"id":                strconv.FormatInt(row.ID, 10),
		"uri":               noteIRI,
		"created_at":        publishedFromNoteOrCreate(act, note),
		"content":           content,
		"visibility":        "public",
		"language":          "en",
		"url":               noteIRI,
		"replies_count":     0,
		"reblogs_count":     0,
		"favourites_count":  0,
		"favourited":        false,
		"reblogged":         false,
		"sensitive":         false,
		"spoiler_text":      "",
		"muted":             false,
		"pinned":            false,
		"bookmarked":        false,
		"account":           acct,
		"media_attachments": []any{},
		"mentions":          []any{},
		"tags":              []any{},
		"emojis":            []any{},
		"card":              nil,
		"poll":              nil,
	}, true
}
