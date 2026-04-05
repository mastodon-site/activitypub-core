package mastodonapi

import (
	"io"
	"net/http"
	"strings"
)

func (s *Server) getTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	root := strings.TrimRight(strings.TrimSpace(s.cfg().PublicBaseURL), "/")
	tagURL := ""
	if root != "" && name != "" {
		tagURL = root + "/tags/" + name
	}
	writeJSONObjectOK(w, map[string]any{
		"name":      name,
		"url":       tagURL,
		"history":   []any{},
		"following": false,
	})
}

func (s *Server) postTagFollow(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	name := r.PathValue("name")
	root := strings.TrimRight(strings.TrimSpace(s.cfg().PublicBaseURL), "/")
	tagURL := ""
	if root != "" && name != "" {
		tagURL = root + "/tags/" + name
	}
	writeJSONObjectOK(w, map[string]any{
		"name":      name,
		"url":       tagURL,
		"history":   []any{},
		"following": true,
	})
}

func (s *Server) postTagUnfollow(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	name := r.PathValue("name")
	root := strings.TrimRight(strings.TrimSpace(s.cfg().PublicBaseURL), "/")
	tagURL := ""
	if root != "" && name != "" {
		tagURL = root + "/tags/" + name
	}
	writeJSONObjectOK(w, map[string]any{
		"name":      name,
		"url":       tagURL,
		"history":   []any{},
		"following": false,
	})
}
