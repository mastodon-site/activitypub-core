package mastodonapi

import "testing"

func TestNormalizeMastodonSearchQuery(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"@user@remote.example", "user@remote.example"},
		{"user@remote.example", "user@remote.example"},
		{"  @alice  ", "alice"},
		{"alice", "alice"},
		{"acct:user@host.test", "acct:user@host.test"},
	}
	for _, tc := range cases {
		if got := normalizeMastodonSearchQuery(tc.in); got != tc.want {
			t.Fatalf("normalizeMastodonSearchQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPickActorHrefFromWebfingerLinks(t *testing.T) {
	const actor = "https://fedi.example/users/a"
	cases := []struct {
		name    string
		links   []webfingerLink
		want    string
		wantErr bool
	}{
		{
			name: "activity json preferred",
			links: []webfingerLink{
				{Rel: "self", Type: "application/activity+json", Href: actor},
			},
			want: actor,
		},
		{
			name: "mastodon style ld json profile",
			links: []webfingerLink{
				{Rel: "self", Type: `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`, Href: actor},
			},
			want: actor,
		},
		{
			name: "activity+json before ld+json",
			links: []webfingerLink{
				{Rel: "self", Type: "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"", Href: "https://wrong"},
				{Rel: "self", Type: "application/activity+json", Href: actor},
			},
			want: actor,
		},
		{
			name: "ld+json fallback",
			links: []webfingerLink{
				{Rel: "http://webfinger.net/rel/avatar", Type: "image/jpeg", Href: "https://fedi.example/avatar.jpg"},
				{Rel: "self", Type: "application/ld+json", Href: actor},
			},
			want: actor,
		},
		{
			name: "rel self case",
			links: []webfingerLink{
				{Rel: "SELF", Type: "application/activity+json", Href: actor},
			},
			want: actor,
		},
		{
			name:    "html self only",
			links:   []webfingerLink{{Rel: "self", Type: "text/html", Href: "https://fedi.example/@a"}},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickActorHrefFromWebfingerLinks(tc.links)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
