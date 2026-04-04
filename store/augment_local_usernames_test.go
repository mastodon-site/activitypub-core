package store

import (
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestMergeCfgLocalUsernames(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://ex.test",
		LocalUsername:  "alice",
		LocalUsernames: []string{"alice"},
	}
	fromDB := map[string]int64{"alice": 1, "bob": 2, "carol": 3}
	mergeCfgLocalUsernames(cfg, fromDB)
	if len(cfg.LocalUsernames) != 3 {
		t.Fatalf("got %v", cfg.LocalUsernames)
	}
	seen := make(map[string]bool)
	for _, u := range cfg.LocalUsernames {
		seen[u] = true
	}
	for _, u := range []string{"alice", "bob", "carol"} {
		if !seen[u] {
			t.Fatalf("missing %q", u)
		}
	}
}

func TestMergeCfgLocalUsernames_emptyListUsesLocalUsername(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL: "https://ex.test",
		LocalUsername: "solo",
	}
	fromDB := map[string]int64{"solo": 1, "other": 2}
	mergeCfgLocalUsernames(cfg, fromDB)
	if len(cfg.LocalUsernames) != 2 {
		t.Fatalf("got %v", cfg.LocalUsernames)
	}
	if !cfg.IsLocalUsername("solo") || !cfg.IsLocalUsername("other") {
		t.Fatalf("local users: %v", cfg.LocalUsernames)
	}
}
