package as2

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ObjectFieldIDType extracts id, type, and raw JSON from an Activity "object" value
// (IRI string or embedded object).
func ObjectFieldIDType(objectRaw json.RawMessage) (id string, typ string, rawJSON []byte, err error) {
	if len(objectRaw) == 0 {
		return "", "", nil, fmt.Errorf("empty object")
	}
	var s string
	if err := json.Unmarshal(objectRaw, &s); err == nil && s != "" {
		return s, "", nil, nil
	}
	rawJSON = objectRaw
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(objectRaw, &obj); err != nil {
		return "", "", nil, fmt.Errorf("object: %w", err)
	}
	id, err = stringField(obj, "id")
	if err != nil {
		return "", "", nil, err
	}
	if tRaw, ok := obj["type"]; ok {
		var ts string
		if json.Unmarshal(tRaw, &ts) == nil {
			typ = NormalizeActivityType(ts)
		} else {
			var ta []any
			if json.Unmarshal(tRaw, &ta) == nil {
				for _, el := range ta {
					if s, ok := el.(string); ok {
						typ = NormalizeActivityType(s)
						if typ != "" {
							break
						}
					}
				}
			}
		}
	}
	return id, typ, rawJSON, nil
}

func stringField(m map[string]json.RawMessage, key string) (string, error) {
	raw, ok := m[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return "", fmt.Errorf("invalid %s", key)
	}
	return s, nil
}

// TombstoneID returns the id from a Delete target that may be a Tombstone or plain IRI.
func TombstoneOrObjectID(objectRaw json.RawMessage) (string, error) {
	id, typ, _, err := ObjectFieldIDType(objectRaw)
	if err != nil {
		return "", err
	}
	_ = typ // may be Tombstone or Note — id is enough for deletes
	return id, nil
}

// ObjectAttributedToMatches checks whether attributedTo equals actorIRI (string form,
// embedded object id, or array of either). Missing attributedTo returns true (implicit author).
func ObjectAttributedToMatches(objectJSON []byte, actorIRI string) bool {
	actorIRI = strings.TrimRight(strings.TrimSpace(actorIRI), "/")
	if actorIRI == "" {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(objectJSON, &obj); err != nil {
		return false
	}
	raw, ok := obj["attributedTo"]
	if !ok {
		return true
	}
	return refContainsActor(raw, actorIRI)
}

func refContainsActor(raw json.RawMessage, actorIRI string) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return strings.TrimRight(strings.TrimSpace(s), "/") == actorIRI
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		id, _ := stringField(m, "id")
		return strings.TrimRight(strings.TrimSpace(id), "/") == actorIRI
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		for _, el := range arr {
			if refContainsActor(el, actorIRI) {
				return true
			}
		}
	}
	return false
}
