package config

import "testing"

func TestLocalUsernameForInboundFollowObject(t *testing.T) {
	cfg := &Config{
		PublicBaseURL:  "https://ex.social",
		LocalUsernames: []string{"alice", "bob_b"},
	}
	cfg.LocalUsername = cfg.LocalUsernames[0]

	tests := []struct {
		iri  string
		want string
		ok   bool
	}{
		{"https://ex.social/@alice", "alice", true},
		{"https://ex.social/@alice/", "alice", true},
		{"https://EX.SOCIAL/users/alice", "alice", true},
		{"https://ex.social/users/bob_b", "bob_b", true},
		{"https://ex.social/users/nobody", "", false},
		{"https://other.test/@alice", "", false},
		{"https://ex.social/.well-known/actor", "", false},
	}
	for _, tc := range tests {
		got, ok := cfg.LocalUsernameForInboundFollowObject(tc.iri)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("LocalUsernameForInboundFollowObject(%q) = (%q, %v); want (%q, %v)", tc.iri, got, ok, tc.want, tc.ok)
		}
	}
}
