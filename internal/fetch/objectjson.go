package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// FetchActivityPubJSON performs a GET for an ActivityStreams object document (Note, Image, etc.).
// maxBytes limits the response body (must be positive; callers should cap around 1–2 MiB).
func FetchActivityPubJSON(ctx context.Context, client *http.Client, policy *Policy, docURL string, cfg *config.Config, maxBytes int64) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("fetch object: nil http client")
	}
	docURL = strings.TrimSpace(docURL)
	if docURL == "" {
		return nil, fmt.Errorf("fetch object: empty url")
	}
	u, err := url.Parse(docURL)
	if err != nil {
		return nil, fmt.Errorf("fetch object: parse: %w", err)
	}
	if policy != nil {
		if err := policy.CheckParsedURL(ctx, u); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", actorAccept)
	req.Header.Set("User-Agent", "activitypub-core/1.0")
	if err := PrepareActorGETRequest(req, cfg); err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("fetch object %s: %s — %s", docURL, resp.Status, strings.TrimSpace(string(slurp)))
	}
	if maxBytes < 1 {
		maxBytes = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("fetch object: response larger than %d bytes", maxBytes)
	}
	return raw, nil
}
