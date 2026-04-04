package as2

import (
	"encoding/json"
	"testing"
)

func TestNormalizeActivityType(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"Create", "Create"},
		{"https://www.w3.org/ns/activitystreams#Create", "Create"},
		{"  Like  ", "Like"},
	} {
		if got := NormalizeActivityType(tc.in); got != tc.want {
			t.Fatalf("NormalizeActivityType(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestPrimaryActivityType(t *testing.T) {
	m := map[string]json.RawMessage{
		"type": json.RawMessage(`["https://www.w3.org/ns/activitystreams#Create","Create"]`),
	}
	if got := PrimaryActivityType(m); got != "Create" {
		t.Fatalf("got %q", got)
	}
}
