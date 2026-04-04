package mastodonapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Stubs for endpoints Mastodon clients often probe after login. All return JSON.

func (s *Server) getCustomEmojis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("[]"))
}

func (s *Server) getAnnouncements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("[]"))
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	out := map[string]any{
		"posting:default:visibility": "public",
		"posting:default:sensitive":  false,
		"posting:default:language":   nil,
		"reading:expand:media":       "default",
		"reading:expand:spoilers":    false,
		"reading:autoplay:gifs":      false,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) getOAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	base := strings.TrimRight(strings.TrimSpace(s.cfg().PublicBaseURL), "/")
	if base == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "Instance is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                           base,
		"authorization_endpoint":           base + "/oauth/authorize",
		"token_endpoint":                   base + "/oauth/token",
		"response_types_supported":         []string{"code"},
		"response_modes_supported":         []string{"query"},
		"grant_types_supported":            []string{"authorization_code"},
		"code_challenge_methods_supported": []string{"S256", "plain"},
		"token_endpoint_auth_methods_supported": []string{
			"client_secret_post",
			"client_secret_basic",
		},
		"scopes_supported": []string{"read", "write", "follow"},
	})
}
