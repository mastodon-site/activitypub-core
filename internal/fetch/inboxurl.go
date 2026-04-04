// Package fetch — resolve ActivityPub inbox endpoints from actor IRIs or inbox URLs.
package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type actorInboxDoc struct {
	Inbox string `json:"inbox"`
}

// InboxURLFromReference returns a shared-inbox HTTPS URL.
// If ref already points at an inbox path, it is normalized; otherwise the actor document is fetched.
func InboxURLFromReference(ctx context.Context, client *http.Client, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty inbox reference")
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("invalid audience url %q", ref)
	}
	u.Fragment = ""
	if looksLikeSharedInboxURL(u) {
		return strings.TrimRight(u.String(), "/"), nil
	}
	return fetchActorInbox(ctx, client, strings.TrimRight(u.String(), "/"))
}

// looksLikeSharedInboxURL reports paths such as /inbox, .../users/x/inbox, or .../inbox/segment (e.g. Mastodon).
func looksLikeSharedInboxURL(u *url.URL) bool {
	p := u.Path
	return strings.HasSuffix(strings.TrimRight(p, "/"), "/inbox") || strings.Contains(p, "/inbox/")
}

func fetchActorInbox(ctx context.Context, client *http.Client, actorURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, actorURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", actorAccept)
	req.Header.Set("User-Agent", "activitypub-core/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch actor: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("fetch actor %s: %s — %s", actorURL, resp.Status, strings.TrimSpace(string(slurp)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var doc actorInboxDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("decode actor json: %w", err)
	}
	if strings.TrimSpace(doc.Inbox) == "" {
		return "", fmt.Errorf("actor %s missing inbox", actorURL)
	}
	return InboxURLFromReference(ctx, client, doc.Inbox)
}
