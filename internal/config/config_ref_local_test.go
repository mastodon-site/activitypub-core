package config

import "testing"

func TestRefAddressesLocalRecipient(t *testing.T) {
	cfg := &Config{
		PublicBaseURL:  "https://ex.social",
		LocalUsernames: []string{"alice", "bob_b"},
	}
	cfg.LocalUsername = cfg.LocalUsernames[0]

	tests := []struct {
		ref string
		ok  bool
	}{
		{"https://ex.social/@alice", true},
		{"https://ex.social/users/bob_b", true},
		{"https://ex.social/.well-known/actor", true},
		{"https://EX.SOCIAL/.well-known/actor", true},
		{"https://other.social/@alice", false},
		{"https://ex.social/users/nobody", false},
	}
	for _, tc := range tests {
		if got := cfg.RefAddressesLocalRecipient(tc.ref); got != tc.ok {
			t.Fatalf("RefAddressesLocalRecipient(%q) = %v; want %v", tc.ref, got, tc.ok)
		}
	}
}
