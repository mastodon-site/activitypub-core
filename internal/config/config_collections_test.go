package config

import "testing"

func TestConfig_IsLocalActorFollowersOrFollowingCollectionIRI(t *testing.T) {
	cfg := &Config{
		PublicBaseURL:  "https://i.test",
		LocalUsername:  "admin",
		LocalUsernames: []string{"admin", "bob"},
	}
	yes := []string{
		cfg.LocalActorFollowersURL("admin"),
		cfg.LocalActorFollowersURL("admin") + "/",
		cfg.LocalActorFollowingURL("bob"),
		"https://I.TEST/@bob/following",
	}
	no := []string{
		cfg.LocalActorProfileURL("admin"),
		cfg.LocalActorOutboxURL("admin"),
		cfg.LocalActorInboxURL("admin"),
		"https://other.test/@admin/followers",
		"https://i.test/@nobody/followers",
	}
	for _, ref := range yes {
		if !cfg.IsLocalActorFollowersOrFollowingCollectionIRI(ref) {
			t.Fatalf("expected true for %q", ref)
		}
	}
	for _, ref := range no {
		if cfg.IsLocalActorFollowersOrFollowingCollectionIRI(ref) {
			t.Fatalf("expected false for %q", ref)
		}
	}
}
