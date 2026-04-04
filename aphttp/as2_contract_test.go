package aphttp

import (
	"encoding/json"
	"testing"
)

func TestContract_activityTypeString_stringForm(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "type", `"Note"`)
	got, err := activityTypeString(raw)
	if err != nil || got != "Note" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestContract_activityTypeString_jsonLdOrderedTypeArray(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "type", `["https://www.w3.org/ns/activitystreams#Create","Create"]`)
	got, err := activityTypeString(raw)
	if err != nil || got != "https://www.w3.org/ns/activitystreams#Create" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestContract_activityTypeString_jsonLdSecondStringWhenFirstNonString(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "type", `[{"not":"string"},"Announce"]`)
	got, err := activityTypeString(raw)
	if err == nil || got != "" {
		t.Fatalf("expected error for non-string first element, got %q %v", got, err)
	}
}

func TestContract_activityTypeString_missing(t *testing.T) {
	raw := map[string]json.RawMessage{}
	_, err := activityTypeString(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestContract_activityTypeString_invalidScalar(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "type", `42`)
	_, err := activityTypeString(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestContract_actorIRIFromActivity_stringForm(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "actor", `"https://remote.example/users/alice"`)
	got, err := actorIRIFromActivity(raw)
	if err != nil || got != "https://remote.example/users/alice" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestContract_actorIRIFromActivity_objectForm(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "actor", `{"id":"https://remote.example/users/alice","type":"Person"}`)
	got, err := actorIRIFromActivity(raw)
	if err != nil || got != "https://remote.example/users/alice" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestContract_actorIRIFromActivity_missing(t *testing.T) {
	raw := map[string]json.RawMessage{}
	_, err := actorIRIFromActivity(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestContract_actorIRIFromActivity_emptyObject(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "actor", `{}`)
	_, err := actorIRIFromActivity(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestContract_jsonStringField_id(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "id", `"https://example.test/activities/1"`)
	got, err := jsonStringField(raw, "id")
	if err != nil || got != "https://example.test/activities/1" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestContract_jsonStringField_rejectsEmptyString(t *testing.T) {
	raw := map[string]json.RawMessage{}
	mustRaw(t, raw, "id", `""`)
	_, err := jsonStringField(raw, "id")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestContract_signingActorMatchesKeyID_httpsWithFragment(t *testing.T) {
	keyID := "https://remote.example/users/alice#main-key"
	actor := "https://remote.example/users/alice"
	if !signingActorMatchesKeyID(keyID, actor) {
		t.Fatal("expected match")
	}
}

func TestContract_signingActorMatchesKeyID_trailingSlashNormalization(t *testing.T) {
	if !signingActorMatchesKeyID("https://a.test/users/bob#main-key", "https://a.test/users/bob/") {
		t.Fatal("expected match")
	}
}

func TestContract_signingActorMatchesKeyID_mismatch(t *testing.T) {
	if signingActorMatchesKeyID("https://a.test/users/one#main-key", "https://a.test/users/two") {
		t.Fatal("expected mismatch")
	}
}

func TestContract_signingActorMatchesKeyID_invalidKeyID(t *testing.T) {
	if signingActorMatchesKeyID("not-a-url", "https://a.test/users/bob") {
		t.Fatal("expected mismatch")
	}
}

func TestContract_isActivityJSONContentType_ldProfileAndBespokeParams(t *testing.T) {
	for _, ct := range []string{
		"application/activity+json",
		"application/activity+json; charset=utf-8",
		`application/ld+json; profile="https://www.w3.org/ns/activitystreams"`,
		"Application/LD+JSON",
	} {
		if !isActivityJSONContentType(ct) {
			t.Fatalf("expected accept %q", ct)
		}
	}
}

func TestContract_isActivityJSONContentType_rejectsPartialMatch(t *testing.T) {
	if isActivityJSONContentType("text/application/activity+json") {
		t.Fatal("substring false positive")
	}
}

func mustRaw(t *testing.T, m map[string]json.RawMessage, key, literalJSON string) {
	t.Helper()
	m[key] = json.RawMessage(literalJSON)
}
