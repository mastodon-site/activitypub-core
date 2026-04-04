package config

import "testing"

func TestConfig_LocalActorInboxURL(t *testing.T) {
	cfg := &Config{PublicBaseURL: "https://social.example"}
	if got, want := cfg.LocalActorInboxURL("alice"), "https://social.example/@alice/inbox"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	cfg.PublicBaseURL = "https://social.example/"
	if got, want := cfg.LocalActorInboxURL("bob"), "https://social.example/@bob/inbox"; got != want {
		t.Fatalf("trailing slash on base: got %q want %q", got, want)
	}
	// Path-escape special username segments
	if got, want := cfg.LocalActorInboxURL("eve/jane"), "https://social.example/@eve%2Fjane/inbox"; got != want {
		t.Fatalf("escape: got %q want %q", got, want)
	}
}

func TestConfig_IsAddressingThisInstanceInbox(t *testing.T) {
	cfg := &Config{
		PublicBaseURL:  "https://i.example",
		LocalUsername:  "legacy",
		LocalUsernames: []string{"alice", "bob"},
	}
	shared := cfg.LocalSharedInboxURL()
	cases := []struct {
		ref  string
		want bool
	}{
		{shared, true},
		{shared + "/", true},
		{" " + shared + "  ", true},
		{cfg.LocalActorInboxURL("alice"), true},
		{cfg.LocalActorInboxURL("alice") + "/", true},
		{cfg.LocalActorInboxURL("bob"), true},
		{"https://other.test/inbox", false},
		{"https://i.example/@nobody/inbox", false},
		{"", false},
		{"https://i.example/", false},
	}
	for _, tc := range cases {
		if got := cfg.IsAddressingThisInstanceInbox(tc.ref); got != tc.want {
			t.Fatalf("IsAddressingThisInstanceInbox(%q) = %v want %v", tc.ref, got, tc.want)
		}
	}
}

func TestConfig_IsAddressingThisInstanceInbox_localUsernameFallback(t *testing.T) {
	cfg := &Config{
		PublicBaseURL: "https://solo.example",
		LocalUsername: "onlyme",
	}
	if !cfg.IsAddressingThisInstanceInbox(cfg.LocalSharedInboxURL()) {
		t.Fatal("shared inbox should match")
	}
	if !cfg.IsAddressingThisInstanceInbox(cfg.LocalActorInboxURL("onlyme")) {
		t.Fatal("fallback LocalUsername inbox should match")
	}
	if cfg.IsAddressingThisInstanceInbox(cfg.LocalActorInboxURL("someone_else")) {
		t.Fatal("non-configured username inbox should not match")
	}
}

func TestConfig_IsAddressingThisInstanceInbox_nilOrEmptyBase(t *testing.T) {
	var nilCfg *Config
	if nilCfg.IsAddressingThisInstanceInbox("https://x/inbox") {
		t.Fatal("nil cfg")
	}
	cfg := &Config{PublicBaseURL: " ", LocalUsernames: []string{"a"}}
	if cfg.IsAddressingThisInstanceInbox("https://x/inbox") {
		t.Fatal("blank base")
	}
}
