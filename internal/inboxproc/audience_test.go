package inboxproc

import (
	"encoding/json"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestActivityShouldApplySideEffects_nilConfig(t *testing.T) {
	fields := map[string]json.RawMessage{"to": mustRawStr(t, `"https://only.remote/u"`)}
	if !activityShouldApplySideEffects(nil, fields) {
		t.Fatal("nil cfg should not filter")
	}
}

func TestActivityShouldApplySideEffects_emptyPublicBaseURL(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "", LocalUsername: "a", LocalUsernames: []string{"a"}}
	fields := map[string]json.RawMessage{"to": mustRawStr(t, `"https://only.remote/u"`)}
	if !activityShouldApplySideEffects(cfg, fields) {
		t.Fatal("empty base URL should not filter")
	}
}

func TestActivityShouldApplySideEffects_emptyAddressing(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://i.test", LocalUsername: "a", LocalUsernames: []string{"a"}}
	if !activityShouldApplySideEffects(cfg, map[string]json.RawMessage{}) {
		t.Fatal("empty addressing should apply")
	}
}

func TestActivityShouldApplySideEffects_localProfileInTo(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://i.test", LocalUsername: "alice", LocalUsernames: []string{"alice"}}
	fields := map[string]json.RawMessage{
		"to": mustRawStr(t, `"`+cfg.LocalActorProfileURL("alice")+`"`),
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		t.Fatal("local profile in to should apply")
	}
}

func TestActivityShouldApplySideEffects_sharedInboxInCc(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://i.test", LocalUsername: "alice", LocalUsernames: []string{"alice"}}
	fields := map[string]json.RawMessage{
		"cc": mustRawArr(t, []string{"https://somewhere.test/u", cfg.LocalSharedInboxURL()}),
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		t.Fatal("shared inbox in cc should apply")
	}
}

func TestActivityShouldApplySideEffects_asPublicOnly(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://i.test", LocalUsername: "a", LocalUsernames: []string{"a"}}
	for _, pub := range []string{
		`"https://www.w3.org/ns/activitystreams#Public"`,
		`"Public"`,
		`"https://example.test/-//feeds/tags/PUBLIC"`,
	} {
		fields := map[string]json.RawMessage{"to": mustRawStr(t, pub)}
		if !activityShouldApplySideEffects(cfg, fields) {
			t.Fatalf("public ref should apply: %s", pub)
		}
	}
}

func TestActivityShouldApplySideEffects_publicAndRemote_applies(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://i.test", LocalUsername: "a", LocalUsernames: []string{"a"}}
	fields := map[string]json.RawMessage{
		"to": mustRawArr(t, []string{
			"https://www.w3.org/ns/activitystreams#Public",
			"https://remote.only/user",
		}),
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		t.Fatal("Public in audience should still apply even with remote refs")
	}
}

func TestActivityShouldApplySideEffects_onlyRemote_noLocalNoPublic(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://i.test", LocalUsername: "alice", LocalUsernames: []string{"alice"}}
	fields := map[string]json.RawMessage{
		"to": mustRawArr(t, []string{"https://a.test/u1", "https://b.test/u2"}),
	}
	if activityShouldApplySideEffects(cfg, fields) {
		t.Fatal("only remote recipients should not apply side effects")
	}
}

func TestActivityShouldApplySideEffects_audienceField(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://i.test", LocalUsername: "bob", LocalUsernames: []string{"bob"}}
	fields := map[string]json.RawMessage{
		"audience": mustRawArr(t, []string{cfg.LocalActorProfileURL("bob")}),
	}
	if !activityShouldApplySideEffects(cfg, fields) {
		t.Fatal("audience should be considered")
	}
}

func TestAudienceIRIs_dedupes(t *testing.T) {
	cfg := map[string]json.RawMessage{
		"to":  mustRawArr(t, []string{"https://x/u", "https://x/u"}),
		"cc":  mustRawStr(t, `"https://x/u"`),
		"bto": mustRawStr(t, `"https://y/v"`),
	}
	got := audienceIRIs(cfg)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func mustRawStr(t *testing.T, s string) json.RawMessage {
	t.Helper()
	return json.RawMessage(s)
}

func mustRawArr(t *testing.T, ss []string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(ss)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
