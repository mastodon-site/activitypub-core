package aphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// HostMeta serves RFC 6415 host-meta plus JSON variant (Mastodon-compatible) for WebFinger discovery.
func (h *Handler) HostMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.TrimSpace(h.cfg.PublicBaseURL) == "" {
		http.Error(w, "server not configured (AP_PUBLIC_BASE_URL)", http.StatusServiceUnavailable)
		return
	}
	tmpl := h.cfg.WebFingerTemplateURL()
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "json") || strings.Contains(accept, "jrd") {
		w.Header().Set("Content-Type", "application/jrd+json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"links": []map[string]string{
				{"rel": "lrdd", "template": tmpl},
			},
		})
		return
	}
	w.Header().Set("Content-Type", "application/xrd+xml; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0">`+
		`<Link rel="lrdd" type="application/jrd+json" template="%s" />`+
		`</XRD>`, escapeXMLAttr(tmpl))
}

// NodeInfoDiscovery serves /.well-known/nodeinfo with links to the concrete schema document.
func (h *Handler) NodeInfoDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.TrimSpace(h.cfg.PublicBaseURL) == "" {
		http.Error(w, "server not configured (AP_PUBLIC_BASE_URL)", http.StatusServiceUnavailable)
		return
	}
	root := strings.TrimRight(strings.TrimSpace(h.cfg.PublicBaseURL), "/")
	href := root + "/nodeinfo/2.0"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"links": []map[string]string{
			{
				"rel":  "http://nodeinfo.diaspora.software/ns/schema/2.0",
				"href": href,
			},
		},
	})
}

// NodeInfo20 serves a NodeInfo 2.0 document for Mastodon-style clients (Ivory, etc.).
func (h *Handler) NodeInfo20(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.TrimSpace(h.cfg.PublicBaseURL) == "" {
		http.Error(w, "server not configured (AP_PUBLIC_BASE_URL)", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	usersTotal, localPosts := h.nodeInfoStats(ctx)
	activeM := usersTotal
	activeH := usersTotal
	desc := strings.TrimSpace(h.cfg.InstanceDescription)
	if desc == "" {
		desc = "ActivityPub (activitypub-core)"
	}
	doc := map[string]any{
		"version": "2.0",
		"software": map[string]string{
			"name":    "activitypub-core",
			"version": h.cfg.SoftwareVersion,
		},
		"protocols": []string{"activitypub"},
		"services": map[string]any{
			"outbound": []any{},
			"inbound":  []any{},
		},
		"usage": map[string]any{
			"users": map[string]any{
				"total":          usersTotal,
				"activeMonth":    activeM,
				"activeHalfyear": activeH,
			},
			"localPosts": localPosts,
		},
		"openRegistrations": h.cfg.OpenRegistrations,
		"metadata": map[string]any{
			"nodeName":        h.cfg.InstanceDisplayName(),
			"nodeDescription": desc,
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(doc)
}

func (h *Handler) nodeInfoStats(ctx context.Context) (usersTotal int, localPosts int) {
	n := len(h.cfg.LocalUsernames)
	if n == 0 && strings.TrimSpace(h.cfg.LocalUsername) != "" {
		n = 1
	}
	if h.st == nil {
		return n, 0
	}
	if len(h.localActorIDs) > 0 {
		usersTotal = len(h.localActorIDs)
	} else {
		usersTotal = n
	}
	u, err := url.Parse(strings.TrimSpace(h.cfg.PublicBaseURL))
	if err != nil || u.Hostname() == "" {
		return usersTotal, 0
	}
	domain := u.Hostname()
	var dbUsers int
	err = h.st.Pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM actors WHERE domain = $1`, domain).Scan(&dbUsers)
	if err == nil && dbUsers > 0 {
		usersTotal = dbUsers
	}
	var posts int
	err = h.st.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM activities a
		INNER JOIN actors act ON act.id = a.actor_id AND act.domain = $1
	`, domain).Scan(&posts)
	if err != nil {
		return usersTotal, 0
	}
	return usersTotal, posts
}

func escapeXMLAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return s
}
