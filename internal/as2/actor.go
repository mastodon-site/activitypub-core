package as2

import (
	"encoding/json"
	"fmt"
)

// ActorIRIFromActivity returns the activity's actor IRI (string or {"id": ...} object).
func ActorIRIFromActivity(m map[string]json.RawMessage) (string, error) {
	raw, ok := m["actor"]
	if !ok {
		return "", fmt.Errorf("missing actor")
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
	return "", fmt.Errorf("actor must be an id string or an object with id")
}
