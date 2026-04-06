package mastodonapi

import (
	"testing"

	"github.com/mastodon-site/activitypub-core/store"
)

func TestStatusHiddenByFilters_substring(t *testing.T) {
	f := []store.MastodonFilter{{
		Phrase:      "spam",
		ContextHome: true,
	}}
	if !statusHiddenByFilters("<p>this is spam here</p>", f, "home") {
		t.Fatal("expected hidden")
	}
	if statusHiddenByFilters("<p>all clear</p>", f, "home") {
		t.Fatal("expected visible")
	}
}

func TestStatusHiddenByFilters_wholeWord(t *testing.T) {
	f := []store.MastodonFilter{{
		Phrase:      "cat",
		WholeWord:   true,
		ContextHome: true,
	}}
	if !statusHiddenByFilters("<p>a cat sat</p>", f, "home") {
		t.Fatal("expected hidden for word cat")
	}
	if statusHiddenByFilters("<p>concatenate</p>", f, "home") {
		t.Fatal("substring cat in concatenate should not match whole word")
	}
}
