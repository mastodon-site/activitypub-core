package as2

import (
	"encoding/json"
	"strings"
)

// NormalizeActivityType strips fragments and trailing noise from a type string
// (e.g. "https://www.w3.org/ns/activitystreams#Create" → "Create").
func NormalizeActivityType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	if i := strings.LastIndex(t, "#"); i >= 0 {
		return t[i+1:]
	}
	return t
}

// PrimaryActivityType returns the first concrete AS2 type from the JSON-LD "type" field.
func PrimaryActivityType(m map[string]json.RawMessage) string {
	raw, ok := m["type"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return NormalizeActivityType(s)
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, el := range arr {
			if s, ok := el.(string); ok && s != "" {
				return NormalizeActivityType(s)
			}
		}
	}
	return ""
}
