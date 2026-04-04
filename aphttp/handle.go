package aphttp

import (
	"net/url"
	"strings"
)

// parseAtHandle returns the local username if handle is "@user" (path-unescaped).
func parseAtHandle(handle string) (username string, ok bool) {
	handle = strings.TrimSpace(handle)
	if !strings.HasPrefix(handle, "@") || len(handle) < 2 {
		return "", false
	}
	rest := handle[1:]
	u, err := url.PathUnescape(rest)
	if err != nil {
		return "", false
	}
	u = strings.Trim(u, "/")
	if u == "" {
		return "", false
	}
	return u, true
}
