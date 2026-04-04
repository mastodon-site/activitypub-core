package httpsig

import (
	"fmt"
	"net/http"
	"strings"
)

// BuildSigningString constructs the string that is hashed and signed (Cavage http-signatures).
// headerNames order follows the Signature `headers` parameter (e.g. "(request-target) host date digest content-type").
func BuildSigningString(r *http.Request, headerNames []string) (string, error) {
	var lines []string
	for _, name := range headerNames {
		name = strings.TrimSpace(name)
		lower := strings.ToLower(name)
		if lower == "(request-target)" {
			path := r.URL.EscapedPath()
			if path == "" {
				path = "/"
			}
			if rq := r.URL.RawQuery; rq != "" {
				path += "?" + rq
			}
			target := strings.ToLower(r.Method) + " " + path
			lines = append(lines, "(request-target): "+target)
			continue
		}
		can := http.CanonicalHeaderKey(lower)
		vals := r.Header.Values(can)
		if len(vals) == 0 {
			return "", fmt.Errorf("missing signed header %q", name)
		}
		lines = append(lines, lower+": "+strings.Join(vals, ", "))
	}
	return strings.Join(lines, "\n"), nil
}
