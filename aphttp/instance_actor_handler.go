package aphttp

import (
	"net/http"
)

// GetInstanceActor serves GET /.well-known/actor (canonical instance actor document).
func (h *Handler) GetInstanceActor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured", http.StatusInternalServerError)
		return
	}
	writeAS2JSON(w, r, instanceActorJSON(h.cfg, h.instancePublicKeyPEM))
}
