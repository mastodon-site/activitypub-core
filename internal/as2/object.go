// Package as2 provides minimal Activity Streams 2.0 JSON helpers.
package as2

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ObjectIRI returns the id from a string or object {"id":...} under key object.
func ObjectIRI(m map[string]json.RawMessage) (string, error) {
	raw, ok := m["object"]
	if !ok {
		return "", fmt.Errorf("missing object")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return s, nil
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.ID != "" {
		return obj.ID, nil
	}
	return "", fmt.Errorf("object must be id string or object with id")
}

// UndoObjectTarget extracts the referenced IRI from an Undo payload (string or embedded object id).
func UndoObjectTarget(m map[string]json.RawMessage) (string, error) {
	return ObjectIRI(m) // same shape: Undo uses "object"
}

// LastPathSegment returns the fragment or last path segment of an IRI for compact ids.
func LastPathSegment(iri string) string {
	iri = strings.TrimRight(strings.TrimSpace(iri), "/")
	if i := strings.LastIndex(iri, "#"); i >= 0 {
		return iri[i+1:]
	}
	i := strings.LastIndex(iri, "/")
	if i >= 0 {
		return iri[i+1:]
	}
	return iri
}
