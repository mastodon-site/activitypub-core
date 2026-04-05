package mastodonapi

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

// decodeAppsRegistrationRequest reads POST /api/v1/apps like Mastodon:
// JSON (application/json) or form (application/x-www-form-urlencoded / multipart/form-data).
// In JSON, redirect_uris may be a string (possibly newline-separated) or an array of strings.
func decodeAppsRegistrationRequest(r *http.Request) (clientName, redirectURIs, scopes, website string, err error) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	ct = strings.ToLower(strings.TrimSpace(ct))

	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 16); err != nil {
			return "", "", "", "", err
		}
		return formAppFields(r)
	}
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return "", "", "", "", err
		}
		return formAppFields(r)
	}

	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return "", "", "", "", err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return "", "", "", "", io.EOF
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return "", "", "", "", err
	}
	if v, ok := m["client_name"]; ok {
		_ = json.Unmarshal(v, &clientName)
		clientName = strings.TrimSpace(clientName)
	}
	if v, ok := m["redirect_uris"]; ok {
		redirectURIs = normalizeRedirectURIsJSON(v)
	}
	if v, ok := m["scopes"]; ok {
		_ = json.Unmarshal(v, &scopes)
		scopes = strings.TrimSpace(scopes)
	}
	if v, ok := m["website"]; ok {
		_ = json.Unmarshal(v, &website)
		website = strings.TrimSpace(website)
	}
	return clientName, redirectURIs, scopes, website, nil
}

func formAppFields(r *http.Request) (clientName, redirectURIs, scopes, website string, err error) {
	return strings.TrimSpace(r.FormValue("client_name")),
		strings.TrimSpace(r.FormValue("redirect_uris")),
		strings.TrimSpace(r.FormValue("scopes")),
		strings.TrimSpace(r.FormValue("website")),
		nil
}

func normalizeRedirectURIsJSON(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, u := range arr {
			u = strings.TrimSpace(u)
			if u != "" {
				parts = append(parts, u)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
