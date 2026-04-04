package aphttp

import (
	"net/http"
	"strings"
)

// WithCORS wraps next so browsers can call the HTTP API from another origin.
// allowedOrigins must be non-empty to enable the wrapper (set AP_CORS_ALLOW_ORIGINS in apd).
//
// Each entry is a full Origin sent by the browser (e.g. https://tools.tootr.co or
// http://localhost:5173). The single entry "*" sets Access-Control-Allow-Origin: * and does not
// enable Access-Control-Allow-Credentials.
func WithCORS(allowedOrigins []string, next http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return next
	}
	wildcard := false
	set := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o == "*" {
			wildcard = true
			continue
		}
		set[o] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		allow := ""
		if wildcard {
			allow = "*"
		} else if origin != "" {
			if _, ok := set[origin]; ok {
				allow = origin
			}
		}
		if allow != "" {
			w.Header().Set("Access-Control-Allow-Origin", allow)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Idempotency-Key")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if allow != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		if r.Method == http.MethodOptions && allow != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
