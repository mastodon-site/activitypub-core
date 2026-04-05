package mastodonapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func Test_timelineLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"default", "", 20},
		{"explicit", "limit=30", 30},
		{"max_cap", "limit=500", 80},
		{"min_bad", "limit=0", 20},
		{"non_numeric", "limit=banana", 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := "/?" + tc.query
			r := httptest.NewRequest("GET", u, nil)
			if g := timelineLimit(r); g != tc.want {
				t.Fatalf("timelineLimit(%q) = %d, want %d", tc.query, g, tc.want)
			}
		})
	}
}

func Test_objectIsNote(t *testing.T) {
	if !objectIsNote(map[string]any{"type": "Note"}) {
		t.Fatal("string Note")
	}
	if !objectIsNote(map[string]any{"type": "nOtE"}) {
		t.Fatal("case fold")
	}
	if !objectIsNote(map[string]any{"type": []any{"https://w3.org/Note"}}) {
		t.Fatal("array type ending in Note")
	}
	if objectIsNote(map[string]any{"type": "Article"}) {
		t.Fatal("reject Article")
	}
	if objectIsNote(map[string]any{}) {
		t.Fatal("reject missing type")
	}
}

func Test_publishedFromNoteOrCreate(t *testing.T) {
	ts := "2020-01-02T15:04:05Z"
	if g := publishedFromNoteOrCreate(map[string]any{}, map[string]any{"published": ts}); g != ts {
		t.Fatalf("note published: got %q", g)
	}
	if g := publishedFromNoteOrCreate(map[string]any{"published": ts}, map[string]any{}); g != ts {
		t.Fatalf("activity published: got %q", g)
	}
	if g := publishedFromNoteOrCreate(map[string]any{}, map[string]any{}); g == "" {
		t.Fatal("fallback RFC3339 expected")
	}
	if _, err := time.Parse(time.RFC3339, publishedFromNoteOrCreate(map[string]any{}, map[string]any{})); err != nil {
		t.Fatalf("fallback not RFC3339: %v", err)
	}
}
