package mastodonapi

import (
	"encoding/json"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

type resolvedCreateStatus struct {
	Row       store.ActivityRow
	Note      map[string]any
	NoteIRI   string
	AuthorIRI string
}

func resolveCreateStatusRow(row *store.ActivityRow) (*resolvedCreateStatus, bool) {
	if row == nil {
		return nil, false
	}
	if !strings.EqualFold(strings.TrimSpace(row.Type), "create") {
		return nil, false
	}
	var act map[string]any
	if err := json.Unmarshal(row.RawJSON, &act); err != nil {
		return nil, false
	}
	obj, ok := act["object"].(map[string]any)
	if !ok || !objectIsNote(obj) {
		return nil, false
	}
	noteIRI, _ := obj["id"].(string)
	noteIRI = strings.TrimSpace(noteIRI)
	if noteIRI == "" {
		return nil, false
	}
	authorIRI, _ := obj["attributedTo"].(string)
	authorIRI = strings.TrimSpace(authorIRI)
	out := &resolvedCreateStatus{
		Row:       *row,
		Note:      obj,
		NoteIRI:   noteIRI,
		AuthorIRI: authorIRI,
	}
	return out, true
}
