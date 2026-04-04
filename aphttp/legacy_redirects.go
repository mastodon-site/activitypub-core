package aphttp

import (
	"net/http"
)

// RedirectInstanceActorAlias issues HTTP 308 from GET /actor to /.well-known/actor
func (h *Handler) RedirectInstanceActorAlias(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, h.cfg.InstanceActorIRI(), http.StatusPermanentRedirect)
}
