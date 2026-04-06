package mastodonapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// mastodonCompatibleVersion is reported to clients via /api/v1|v2/instance (Mastodon 4.x API target).
const mastodonCompatibleVersion = "4.3.0+activitypub-core"

func (s *Server) instanceV1Payload() map[string]any {
	cfg := s.cfg()
	host := s.instanceHost()
	root := ""
	if u := strings.TrimSpace(cfg.PublicBaseURL); u != "" {
		root = strings.TrimRight(u, "/")
	}
	supportedMimes := cfg.EffectiveMediaAllowedMIMETypes()
	maxAtt := cfg.EffectiveMediaMaxAttachmentsPerStatus()
	imgLimit := instanceImageSizeLimit(cfg)
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
				"max_media_attachments":       maxAtt,
				"characters_reserved_per_url": 23,
			},
			"media_attachments": map[string]any{
				"supported_mime_types": supportedMimes,
				"image_size_limit":     imgLimit,
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

func instanceImageSizeLimit(cfg *config.Config) int64 {
	n := int64(0)
	if cfg != nil {
		n = int64(cfg.MediaMaxUploadBytes)
	}
	if n <= 0 {
		n = int64(10 << 20)
	}
	return n
}

func (s *Server) instanceV2Payload() map[string]any {
	cfg := s.cfg()
	host := s.instanceHost()
	supportedMimes := cfg.EffectiveMediaAllowedMIMETypes()
	maxAtt := cfg.EffectiveMediaMaxAttachmentsPerStatus()
	imgLimit := instanceImageSizeLimit(cfg)
	descLimit := 1500
	if cfg != nil && cfg.MediaDescriptionLimit > 0 {
		descLimit = cfg.MediaDescriptionLimit
	}
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
				"max_media_attachments":       maxAtt,
				"characters_reserved_per_url": 23,
			},
			"media_attachments": map[string]any{
				"description_limit":      descLimit,
				"supported_mime_types":   supportedMimes,
				"image_size_limit":       imgLimit,
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
			"mastodon": 1,
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
