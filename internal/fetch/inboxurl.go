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

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// maxInboxResolutionSteps caps HTTP GET actor-document hops when resolving an inbox
// (each hop follows actor JSON "inbox" to the next URL). Must be positive.
const maxInboxResolutionSteps = 5

type actorInboxDoc struct {
	Inbox string `json:"inbox"`
}

// InboxURLFromReference returns a shared-inbox URL for delivery.
// If ref already points at an inbox path, it is normalized; otherwise the actor document is fetched.
// policy propagates SSRF checks (use PolicyFromConfig or TestingPolicy in tests).
// cfg may be nil; when non-nil and cfg.SignOutboundGET, actor document GETs are signed.
//
// Resolution is bounded by maxInboxResolutionSteps fetches and by a cycle check on canonical URLs.
func InboxURLFromReference(ctx context.Context, client *http.Client, policy *Policy, ref string, cfg *config.Config) (string, error) {
	return inboxURLFromReference(ctx, client, policy, ref, cfg, 0, make(map[string]struct{}))
}

func inboxURLFromReference(ctx context.Context, client *http.Client, policy *Policy, ref string, cfg *config.Config, fetchDepth int, seen map[string]struct{}) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty inbox reference")
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("invalid audience url %q", ref)
	}
	u.Fragment = ""
	canon := strings.TrimRight(u.String(), "/")
	if canon == "" {
		return "", fmt.Errorf("invalid audience url %q", ref)
	}
	if _, dup := seen[canon]; dup {
		return "", fmt.Errorf("inbox: resolution cycle involving %q", canon)
	}
	seen[canon] = struct{}{}

	if policy != nil {
		if err := policy.CheckParsedURL(ctx, u); err != nil {
			return "", err
		}
	}
	if looksLikeSharedInboxURL(u) {
		return canon, nil
	}
	if fetchDepth >= maxInboxResolutionSteps {
		return "", fmt.Errorf("inbox: resolution exceeded %d actor fetch(es)", maxInboxResolutionSteps)
	}
	return fetchActorInbox(ctx, client, policy, canon, cfg, fetchDepth+1, seen)
}

// looksLikeSharedInboxURL reports paths such as /inbox, .../users/x/inbox, or .../inbox/segment (e.g. Mastodon).
func looksLikeSharedInboxURL(u *url.URL) bool {
	p := u.Path
	return strings.HasSuffix(strings.TrimRight(p, "/"), "/inbox") || strings.Contains(p, "/inbox/")
}

func fetchActorInbox(ctx context.Context, client *http.Client, policy *Policy, actorURL string, cfg *config.Config, nextFetchDepth int, seen map[string]struct{}) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, actorURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", actorAccept)
	req.Header.Set("User-Agent", "activitypub-core/1.0")
	if err := PrepareActorGETRequest(req, cfg); err != nil {
		return "", err
	}

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
	return inboxURLFromReference(ctx, client, policy, doc.Inbox, cfg, nextFetchDepth, seen)
}
