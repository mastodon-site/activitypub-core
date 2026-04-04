package inboxproc

import (
	"encoding/json"
	"strings"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// activityShouldApplySideEffects returns false when the activity explicitly addresses
// only remote parties (no local actor, shared inbox, or ActivityStreams Public).
// Empty addressing fields mean "unspecified" and are treated as true.
func activityShouldApplySideEffects(cfg *config.Config, fields map[string]json.RawMessage) bool {
	if cfg == nil || cfg.PublicBaseURL == "" {
		return true
	}
	refs := audienceIRIs(fields)
	if len(refs) == 0 {
		return true
	}
	shared := strings.TrimRight(cfg.LocalSharedInboxURL(), "/")
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if isASPublicRef(r) {
			return true
		}
		if strings.TrimRight(r, "/") == shared {
			return true
		}
		if _, ok := cfg.LocalUsernameForActorURL(r); ok {
			return true
		}
	}
	return false
}

func isASPublicRef(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "public" || lower == "as:public" {
		return true
	}
	return strings.Contains(lower, "#public") || strings.HasSuffix(lower, "/public") ||
		strings.Contains(lower, "activitystreams#public")
}

func audienceIRIs(fields map[string]json.RawMessage) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, key := range []string{"to", "cc", "bto", "bcc", "audience"} {
		raw, ok := fields[key]
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
