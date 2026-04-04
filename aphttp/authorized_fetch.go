package aphttp

import (
	"fmt"
	"net/http"

	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
)

func (h *Handler) verifyAuthorizedFetch(r *http.Request) error {
	sig := r.Header.Get("Signature")
	if sig == "" {
		return fmt.Errorf("missing signature")
	}
	params, err := httpsig.ParseSignatureHeader(sig)
	if err != nil {
		return err
	}
	keyID := params["keyid"]
	if keyID == "" {
		return fmt.Errorf("missing keyId")
	}
	pub, err := fetch.PublicKeyForKeyID(r.Context(), h.fetchClient, h.fetchPolicy, keyID, h.cfg)
	if err != nil {
		return err
	}
	return httpsig.VerifyGet(r, pub)
}
