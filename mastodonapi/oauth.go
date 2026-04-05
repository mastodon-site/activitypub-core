package mastodonapi

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"mime"
	"mime/multipart"
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

// parseOAuthTokenParams reads RFC 6749 token endpoint parameters from the POST
// body (form, JSON, or multipart), query string, and client_secret_basic.
//
// Some native clients send JSON with Content-Type application/x-www-form-urlencoded
// (or omit a JSON Content-Type). url.ParseQuery on a JSON payload yields no
// grant_type and breaks login with invalid_request — so we sniff JSON objects
// and fall back from mislabeled form bodies.
func parseOAuthTokenParams(r *http.Request) (url.Values, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	body := bytes.TrimSpace(raw)
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}) // UTF-8 BOM

	vals := make(url.Values)
	for k, v := range r.URL.Query() {
		vals[k] = append(vals[k], v...)
	}

	ct, ctParams, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		ct = ""
		ctParams = nil
	}

	mergeJSONFields := func(b []byte) error {
		var payload struct {
			GrantType    string `json:"grant_type"`
			Code         string `json:"code"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			RedirectURI  string `json:"redirect_uri"`
			CodeVerifier string `json:"code_verifier"`
			Scope        string `json:"scope"`
		}
		if err := json.Unmarshal(b, &payload); err != nil {
			return err
		}
		merge := func(key, s string) {
			s = strings.TrimSpace(s)
			if s != "" && vals.Get(key) == "" {
				vals.Set(key, s)
			}
		}
		merge("grant_type", payload.GrantType)
		merge("code", payload.Code)
		merge("client_id", payload.ClientID)
		if strings.TrimSpace(payload.ClientSecret) != "" && vals.Get("client_secret") == "" {
			vals.Set("client_secret", strings.TrimSpace(payload.ClientSecret))
		}
		merge("redirect_uri", payload.RedirectURI)
		merge("code_verifier", payload.CodeVerifier)
		merge("scope", payload.Scope)
		return nil
	}

	mergeForm := func(b []byte) error {
		if len(b) == 0 {
			return nil
		}
		q, err := url.ParseQuery(string(b))
		if err != nil {
			return err
		}
		for k, v := range q {
			if vals.Get(k) == "" {
				vals[k] = append(vals[k], v...)
			}
		}
		return nil
	}

	mergeMultipart := func(b []byte, boundary string) error {
		if boundary == "" {
			return fmt.Errorf("multipart: missing boundary")
		}
		mr := multipart.NewReader(bytes.NewReader(b), boundary)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			name := part.FormName()
			if name == "" {
				_, _ = io.Copy(io.Discard, part)
				continue
			}
			buf, err := io.ReadAll(io.LimitReader(part, 1<<20))
			if err != nil {
				return err
			}
			if vals.Get(name) == "" {
				vals.Set(name, string(buf))
			}
		}
	}

	bodyLooksJSONObject := len(body) > 0 && body[0] == '{'
	jsonCT := ct == "application/json" || strings.HasSuffix(ct, "+json")

	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		boundary := ctParams["boundary"]
		if len(body) > 0 {
			if err := mergeMultipart(body, boundary); err != nil {
				return nil, err
			}
		}
		if vals.Get("grant_type") == "" && bodyLooksJSONObject {
			if err := mergeJSONFields(body); err != nil {
				return nil, err
			}
		}

	case jsonCT:
		if len(body) > 0 {
			if err := mergeJSONFields(body); err != nil {
				return nil, err
			}
		}

	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		if len(body) > 0 {
			if err := mergeForm(body); err != nil {
				return nil, err
			}
		}
		if vals.Get("grant_type") == "" && bodyLooksJSONObject {
			if err := mergeJSONFields(body); err != nil {
				return nil, err
			}
		}

	default:
		if len(body) == 0 {
			break
		}
		if bodyLooksJSONObject {
			if err := mergeJSONFields(body); err != nil {
				return nil, err
			}
		} else if err := mergeForm(body); err != nil {
			return nil, err
		}
	}

	if cid, secret, ok := r.BasicAuth(); ok {
		if vals.Get("client_id") == "" {
			vals.Set("client_id", cid)
		}
		if vals.Get("client_secret") == "" {
			vals.Set("client_secret", secret)
		}
	}
	return vals, nil
}

func oauthScopesSubset(requested, allowed string) bool {
	req := strings.Fields(strings.ReplaceAll(strings.TrimSpace(requested), "+", " "))
	if len(req) == 0 {
		return true
	}
	allow := strings.Fields(strings.ReplaceAll(strings.TrimSpace(allowed), "+", " "))
	set := make(map[string]struct{}, len(allow))
	for _, s := range allow {
		set[s] = struct{}{}
	}
	for _, s := range req {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

func (s *Server) postOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthTokenError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	vals, err := parseOAuthTokenParams(r)
	if err != nil {
		oauthTokenError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	grantType := strings.TrimSpace(vals.Get("grant_type"))
	if grantType == "" {
		oauthTokenError(w, http.StatusBadRequest, "invalid_request", "grant_type is required")
		return
	}
	switch strings.ToLower(grantType) {
	case "authorization_code":
		s.postOAuthTokenAuthorizationCode(w, r, vals)
	case "client_credentials":
		s.postOAuthTokenClientCredentials(w, r, vals)
	default:
		oauthTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "grant type is not supported")
	}
}

func (s *Server) postOAuthTokenAuthorizationCode(w http.ResponseWriter, r *http.Request, vals url.Values) {
	code := strings.TrimSpace(vals.Get("code"))
	clientID := strings.TrimSpace(vals.Get("client_id"))
	clientSecret := vals.Get("client_secret")
	redirectURI := strings.TrimSpace(vals.Get("redirect_uri"))
	verifier := strings.TrimSpace(vals.Get("code_verifier"))
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
	writeOAuthTokenOK(w, rawTok, row.Scopes)
}

func (s *Server) postOAuthTokenClientCredentials(w http.ResponseWriter, r *http.Request, vals url.Values) {
	clientID := strings.TrimSpace(vals.Get("client_id"))
	clientSecret := vals.Get("client_secret")
	redirectURI := strings.TrimSpace(vals.Get("redirect_uri"))
	scopeReq := strings.TrimSpace(vals.Get("scope"))
	if clientID == "" || redirectURI == "" {
		oauthTokenError(w, http.StatusBadRequest, "invalid_request", "client_id, client_secret, and redirect_uri are required")
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
	if !store.RedirectURIAllowed(app.RedirectURIs, redirectURI) {
		oauthTokenError(w, http.StatusBadRequest, "invalid_request", "invalid redirect_uri")
		return
	}
	if scopeReq == "" {
		scopeReq = "read"
	}
	if !oauthScopesSubset(scopeReq, app.Scopes) {
		oauthTokenError(w, http.StatusBadRequest, "invalid_scope", "The requested scope is invalid, unknown, or malformed.")
		return
	}
	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		oauthTokenError(w, http.StatusInternalServerError, "server_error", "could not begin transaction")
		return
	}
	defer tx.Rollback(ctx)
	rawTok, err := store.InsertAppAccessTokenTx(ctx, tx, app.ID, scopeReq)
	if err != nil {
		oauthTokenError(w, http.StatusInternalServerError, "server_error", "could not create access token")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		oauthTokenError(w, http.StatusInternalServerError, "server_error", "could not commit transaction")
		return
	}
	writeOAuthTokenOK(w, rawTok, scopeReq)
}

func writeOAuthTokenOK(w http.ResponseWriter, rawTok, scopes string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": rawTok,
		"token_type":   "Bearer",
		"scope":        scopes,
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
