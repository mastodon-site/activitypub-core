package aphttp

import (
	"net/http"
)

// GetLocalActor serves GET /{handle} when handle is @username
func (h *Handler) GetLocalActor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured", http.StatusInternalServerError)
		return
	}
	handle := r.PathValue("handle")
	username, ok := parseAtHandle(handle)
	if !ok || !h.IsLocalActor(username) {
		http.NotFound(w, r)
		return
	}
	writeAS2JSON(w, r, localActorJSON(h.cfg, username, h.actorPublicKeyPEM))
}
