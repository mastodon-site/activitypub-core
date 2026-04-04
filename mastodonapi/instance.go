package mastodonapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

const mastodonCompatibleVersion = "4.2.0+activitypub-core"

func (s *Server) instanceV1Payload() map[string]any {
	host := s.instanceHost()
	root := ""
	if u := strings.TrimSpace(s.cfg().PublicBaseURL); u != "" {
		root = strings.TrimRight(u, "/")
	}
	out := map[string]any{
		"uri":               host,
		"title":             host,
		"short_description": "",
		"description":       "",
		"email":             "",
		"version":           mastodonCompatibleVersion,
		"urls":              map[string]any{},
		"stats":             map[string]any{"user_count": 0, "status_count": 0, "domain_count": 0},
		"thumbnail":         nil,
		"languages":         []any{"en"},
		"registrations":     false,
		"approval_required": true,
		"invites_enabled":   false,
		"contact_account":   nil,
		"rules":             []any{},
		"domain":            host,
		"configuration": map[string]any{
			"urls": map[string]any{},
			"accounts": map[string]any{
				"max_featured_tags":   0,
				"max_pinned_statuses": 0,
			},
			"statuses": map[string]any{
				"max_characters":              500,
				"max_media_attachments":       4,
				"characters_reserved_per_url": 23,
			},
			"media_attachments": map[string]any{
				"supported_mime_types": []string{
					"image/jpeg", "image/png", "image/gif", "image/webp",
				},
				"image_size_limit": int64(8 << 20),
			},
			"vapid": map[string]any{
				"public_key": nil,
			},
		},
	}
	if root != "" {
		out["uri"] = root
	}
	return out
}

func (s *Server) instanceV2Payload() map[string]any {
	host := s.instanceHost()
	supportedMimes := []string{"image/jpeg", "image/png", "image/gif", "image/webp"}
	return map[string]any{
		"domain":      host,
		"title":       host,
		"version":     mastodonCompatibleVersion,
		"source_url":  "https://github.com/mastodon-site/activitypub-core",
		"description": "",
		"usage": map[string]any{
			"users": map[string]any{"active_month": 0},
		},
		"thumbnail": map[string]any{
			"url":      nil,
			"blurhash": nil,
			"versions": map[string]any{},
		},
		"icon":      []any{},
		"languages": []any{"en"},
		"configuration": map[string]any{
			"urls": map[string]any{},
			"vapid": map[string]any{
				"public_key": "",
			},
			"accounts": map[string]any{
				"max_featured_tags":   0,
				"max_pinned_statuses": 0,
			},
			"statuses": map[string]any{
				"max_characters":              500,
				"max_media_attachments":       4,
				"characters_reserved_per_url": 23,
			},
			"media_attachments": map[string]any{
				"description_limit":      1500,
				"supported_mime_types":   supportedMimes,
				"image_size_limit":       int64(8 << 20),
				"image_matrix_limit":     int64(33177600),
				"video_matrix_limit":     int64(8294400),
				"video_size_limit":       int64(100 << 20),
				"video_frame_rate_limit": 120,
			},
			"polls": map[string]any{
				"max_options":               4,
				"max_characters_per_option": 50,
				"min_expiration":            300,
				"max_expiration":            2629746,
			},
			"translation": map[string]any{
				"enabled": false,
			},
			"limited_federation": false,
		},
		"registrations": map[string]any{
			"enabled":           false,
			"approval_required": true,
			"reason_required":   false,
			"message":           nil,
			"url":               nil,
		},
		"api_versions": map[string]any{
			"mastodon": 2,
		},
		"contact": map[string]any{
			"email":   "",
			"account": nil,
		},
		"rules": []any{},
	}
}

func (s *Server) getInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.instanceHost() == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "Instance is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.instanceV1Payload())
}

func (s *Server) getInstanceV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.instanceHost() == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "Instance is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.instanceV2Payload())
}

func (s *Server) getInstanceExtendedDescription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.instanceHost() == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "Instance is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content":    "",
		"updated_at": nil,
	})
}
