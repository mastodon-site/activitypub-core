package mastodonapi

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/mastodon-site/activitypub-core/store"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func plainTextFromHTMLStatus(html string) string {
	s := htmlTagPattern.ReplaceAllString(html, " ")
	return strings.TrimSpace(s)
}

func statusHiddenByFilters(htmlContent string, filters []store.MastodonFilter, contextName string) bool {
	if len(filters) == 0 {
		return false
	}
	plain := strings.ToLower(plainTextFromHTMLStatus(htmlContent))
	if plain == "" {
		return false
	}
	for _, f := range filters {
		var use bool
		switch contextName {
		case "home":
			use = f.ContextHome
		case "public":
			use = f.ContextPublic
		case "thread":
			use = f.ContextThread
		case "account":
			use = f.ContextAccount
		case "notifications":
			use = f.ContextNotifications
		default:
			use = f.ContextHome
		}
		if !use {
			continue
		}
		ph := strings.ToLower(strings.TrimSpace(f.Phrase))
		if ph == "" {
			continue
		}
		if f.WholeWord {
			if !containsWholeWordFold(plain, ph) {
				continue
			}
		} else if !strings.Contains(plain, ph) {
			continue
		}
		return true
	}
	return false
}

func containsWholeWordFold(hay, needle string) bool {
	if needle == "" {
		return false
	}
	for _, w := range strings.FieldsFunc(hay, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if w == needle {
			return true
		}
	}
	return false
}
