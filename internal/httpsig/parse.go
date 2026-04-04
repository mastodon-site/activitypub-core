package httpsig

import (
	"fmt"
	"strings"
)

// ParseSignatureHeader parses the HTTP Signatures (Cavage-style) Signature header value.
func ParseSignatureHeader(raw string) (map[string]string, error) {
	out := make(map[string]string)
	i := 0
	raw = strings.TrimSpace(raw)
	for i < len(raw) {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == ',' || raw[i] == '\t') {
			i++
		}
		if i >= len(raw) {
			break
		}
		j := i
		for j < len(raw) && raw[j] != '=' {
			j++
		}
		if j >= len(raw) {
			return nil, fmt.Errorf("signature param: expected =")
		}
		key := strings.TrimSpace(raw[i:j])
		i = j + 1
		if i >= len(raw) {
			return nil, fmt.Errorf("signature param %q: missing value", key)
		}
		var val string
		if raw[i] == '"' {
			i++
			sb := new(strings.Builder)
			for i < len(raw) {
				if raw[i] == '\\' && i+1 < len(raw) {
					i++
					if i < len(raw) {
						sb.WriteByte(raw[i])
						i++
					}
					continue
				}
				if raw[i] == '"' {
					i++
					val = sb.String()
					goto next
				}
				sb.WriteByte(raw[i])
				i++
			}
			return nil, fmt.Errorf("unclosed quote in signature header")
		} else {
			start := i
			for i < len(raw) && raw[i] != ',' {
				i++
			}
			val = strings.TrimSpace(raw[start:i])
		}
	next:
		out[strings.ToLower(key)] = val
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty signature header")
	}
	return out, nil
}
