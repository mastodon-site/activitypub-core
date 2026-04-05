package mastodonapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mastodon-site/activitypub-core/store"
)

// OAuth token errors must be JSON (RFC 6749 §5.2). Plain text breaks Mastodon clients (e.g. Ivory).
func oauthTokenError(w http.ResponseWriter, status int, errCode, description string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             errCode,
		"error_description": description,
	})
}

// oauthAuthorizeHTML serves text/html for browser OAuth steps (not JSON).
func oauthAuthorizeHTML(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Authorize</title><p>%s</p>`,
		html.EscapeString(message))
}

func (s *Server) getOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		oauthAuthorizeHTML(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	q := r.URL.Query()
	if q.Get("response_type") != "code" {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "response_type must be code")
		return
	}
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "client_id and redirect_uri are required")
		return
	}
	app, err := store.OAuthApplicationByClientID(r.Context(), s.Pool, clientID)
	if err != nil {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "Unknown client_id")
		return
	}
	if !store.RedirectURIAllowed(app.RedirectURIs, redirectURI) {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "Invalid redirect_uri")
		return
	}
	host := s.instanceHost()
	tpl := template.Must(template.New("oauth").Parse(oauthFormHTML))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, map[string]string{
		"ClientID":            clientID,
		"RedirectURI":         redirectURI,
		"Scope":               q.Get("scope"),
		"State":               q.Get("state"),
		"CodeChallenge":       q.Get("code_challenge"),
		"CodeChallengeMethod": q.Get("code_challenge_method"),
		"InstanceHost":        host,
		"ResponseType":        "code",
	})
}

const oauthFormHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Authorize</title></head><body>
<h1>Sign in — @{{.InstanceHost}}</h1>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="scope" value="{{.Scope}}">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
<input type="hidden" name="response_type" value="{{.ResponseType}}">
<p><label>Username <input name="username" autocomplete="username" required></label></p>
<p><label>Password <input name="password" type="password" autocomplete="current-password" required></label></p>
<p><button type="submit">Authorize</button></p>
</form></body></html>
`

func (s *Server) postOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthAuthorizeHTML(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "Invalid form")
		return
	}
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	state := r.FormValue("state")
	scope := r.FormValue("scope")
	chal := r.FormValue("code_challenge")
	chalMeth := r.FormValue("code_challenge_method")

	app, err := store.OAuthApplicationByClientID(r.Context(), s.Pool, clientID)
	if err != nil {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "Unknown client_id")
		return
	}
	if !store.RedirectURIAllowed(app.RedirectURIs, redirectURI) {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "Invalid redirect_uri")
		return
	}
	dom := s.instanceHost()
	actorID, err := store.AuthenticateLocalAccount(r.Context(), s.Pool, dom, username, password)
	if err != nil {
		oauthAuthorizeHTML(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}
	if scope == "" {
		scope = app.Scopes
	}
	code, err := store.InsertAuthorizationCode(r.Context(), s.Pool, app.ID, actorID, redirectURI, scope, chal, chalMeth)
	if err != nil {
		oauthAuthorizeHTML(w, http.StatusInternalServerError, "Could not create authorization code")
		return
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		oauthAuthorizeHTML(w, http.StatusBadRequest, "Invalid redirect URI")
		return
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Server) postOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthTokenError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthTokenError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}
	if r.FormValue("grant_type") != "authorization_code" {
		oauthTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	code := r.FormValue("code")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")
	redirectURI := r.FormValue("redirect_uri")
	verifier := r.FormValue("code_verifier")
	if code == "" || clientID == "" || redirectURI == "" {
		oauthTokenError(w, http.StatusBadRequest, "invalid_request", "code, client_id, and redirect_uri are required")
		return
	}
	app, err := store.OAuthApplicationByClientID(r.Context(), s.Pool, clientID)
	if err != nil {
		oauthTokenError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}
	if !store.VerifyClientSecret(app, clientSecret) {
		oauthTokenError(w, http.StatusUnauthorized, "invalid_client", "invalid client_secret")
		return
	}

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		oauthTokenError(w, http.StatusInternalServerError, "server_error", "could not begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	row, err := store.ConsumeAuthorizationCode(ctx, tx, code, redirectURI)
	if err != nil {
		oauthTokenError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}
	if row.ApplicationID != app.ID {
		oauthTokenError(w, http.StatusBadRequest, "invalid_grant", "authorization code was issued to another client")
		return
	}
	if err := pkceVerify(verifier, row); err != nil {
		oauthTokenError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	rawTok, err := store.InsertAccessTokenTx(ctx, tx, row.ApplicationID, row.ActorID, row.Scopes)
	if err != nil {
		oauthTokenError(w, http.StatusInternalServerError, "server_error", "could not create access token")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		oauthTokenError(w, http.StatusInternalServerError, "server_error", "could not commit transaction")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": rawTok,
		"token_type":   "Bearer",
		"scope":        row.Scopes,
		"created_at":   time.Now().Unix(),
	})
}

func pkceVerify(verifier string, row store.AuthCodeRow) error {
	if row.Challenge == "" {
		return nil
	}
	v := strings.TrimSpace(verifier)
	if v == "" {
		return fmt.Errorf("code_verifier required")
	}
	meth := strings.ToUpper(strings.TrimSpace(row.ChallengeMeth))
	if meth == "" {
		meth = "S256"
	}
	switch meth {
	case "S256":
		sum := sha256.Sum256([]byte(v))
		expect := base64.RawURLEncoding.EncodeToString(sum[:])
		if subtle.ConstantTimeCompare([]byte(expect), []byte(row.Challenge)) != 1 {
			return fmt.Errorf("invalid code_verifier")
		}
		return nil
	case "PLAIN":
		if subtle.ConstantTimeCompare([]byte(v), []byte(row.Challenge)) != 1 {
			return fmt.Errorf("invalid code_verifier")
		}
		return nil
	default:
		return fmt.Errorf("unsupported code_challenge_method")
	}
}

// postOAuthRevoke handles POST /oauth/revoke (RFC 7009-style token revocation probe from clients).
// Tokens are not revoked server-side yet; the endpoint returns 200 so clients do not break.
func (s *Server) postOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}
