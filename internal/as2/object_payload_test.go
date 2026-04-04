package as2

import (
	"encoding/json"
	"testing"
)

func TestObjectFieldIDType_embedded(t *testing.T) {
	raw := json.RawMessage(`{"id":"https://ex/note/1","type":"Note","content":"hi"}`)
	id, typ, full, err := ObjectFieldIDType(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id != "https://ex/note/1" || typ != "Note" || string(full) != string(raw) {
		t.Fatalf("id=%q typ=%q full=%s", id, typ, full)
	}
}

func TestObjectFieldIDType_iriOnly(t *testing.T) {
	id, typ, full, err := ObjectFieldIDType(json.RawMessage(`"https://ex/note/2"`))
	if err != nil {
		t.Fatal(err)
	}
	if id != "https://ex/note/2" || typ != "" || full != nil {
		t.Fatalf("id=%q typ=%q full=%v", id, typ, full)
	}
}

func TestObjectAttributedToMatches(t *testing.T) {
	actor := "https://ex/users/a"
	note := []byte(`{"id":"https://ex/n/1","attributedTo":"` + actor + `"}`)
	if !ObjectAttributedToMatches(note, actor) {
		t.Fatal("expected match")
	}
	if ObjectAttributedToMatches(note, "https://evil/user") {
		t.Fatal("expected no match")
	}
	if !ObjectAttributedToMatches([]byte(`{"id":"https://ex/n/2"}`), actor) {
		t.Fatal("missing attributedTo should match (implicit author)")
	}
}
