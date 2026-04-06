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

// requireAuthorizedFetch writes 401 unless AP_REQUIRE_AUTHORIZED_FETCH is off or the request passes HTTPSig verification.
func (h *Handler) requireAuthorizedFetch(w http.ResponseWriter, r *http.Request) bool {
	if h.cfg == nil || !h.cfg.RequireAuthorizedFetch {
		return true
	}
	if err := h.verifyAuthorizedFetch(r); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}
