// Package aphttp serves ActivityPub HTTP surfaces (WebFinger, actors, inboxes).
package aphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mastodon-site/activitypub-core/blobs"
	"github.com/mastodon-site/activitypub-core/internal/actorkey"
	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/httpsig"
	"github.com/mastodon-site/activitypub-core/queue"
	"github.com/mastodon-site/activitypub-core/queue/sqlqueue"
	"github.com/mastodon-site/activitypub-core/store"
)

// Deps wires optional persistence and the job backend (both required for durable inbox handling).
type Deps struct {
	Store *store.Postgres
	Queue queue.Backend
	Blobs blobs.Store
}

// Handler bundles AP HTTP handlers for mounting on the API mux.
type Handler struct {
	cfg *config.Config
	// actorPublicKeyPEM is PKIX PEM for JSON-LD publicKey.publicKeyPem; empty means use stub in GetActor.
	actorPublicKeyPEM    string
	fetchClient          *http.Client
	fetchPolicy          *fetch.Policy
	inboxMaxBody         int
	st                   *store.Postgres
	q                    queue.Backend
	blobs                blobs.Store
	localActorIDs        map[string]int64 // username -> actors.id
	instancePublicKeyPEM string
}

// New creates AP HTTP handlers. cfg.PublicBaseURL must be set for meaningful responses.
// If cfg sets actor key paths, the PEM for the actor document is loaded at startup.
// When deps.Store is set, each configured local username is upserted into actors and localActorIDs is populated.
func New(cfg *config.Config, deps Deps) (*Handler, error) {
	pol := fetch.PolicyFromConfig(cfg)
	h := &Handler{
		cfg:          cfg,
		st:           deps.Store,
		q:            deps.Queue,
		blobs:        deps.Blobs,
		fetchClient:  fetch.NewHTTPClientForPolicy(pol, 30*time.Second),
		fetchPolicy:  pol,
		inboxMaxBody: cfg.InboxMaxBody,
	}
	if cfg.ActorPrivateKeyPath != "" {
		pub, err := actorkey.ActorPublicKeyPEMForConfig(cfg)
		if err != nil {
			return nil, err
		}
		h.actorPublicKeyPEM = pub
	}
	h.instancePublicKeyPEM = h.actorPublicKeyPEM
	if strings.TrimSpace(cfg.InstanceActorPrivateKeyPath) != "" {
		instPriv, err := actorkey.LoadPrivateKeyFromFile(cfg.InstanceActorPrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("instance actor key: %w", err)
		}
		instPub, err := actorkey.PublicKeyPEMFromPrivate(instPriv)
		if err != nil {
			return nil, err
		}
		h.instancePublicKeyPEM = instPub
	}
	if h.st != nil {
		h.localActorIDs = make(map[string]int64)
		localNames := cfg.LocalUsernames
		if len(localNames) == 0 && strings.TrimSpace(cfg.LocalUsername) != "" {
			localNames = []string{cfg.LocalUsername}
		}
		for _, uname := range localNames {
			id, err := store.EnsureLocalActor(context.Background(), h.st.Pool, cfg, uname, h.actorPublicKeyPEM)
			if err != nil {
				return nil, err
			}
			h.localActorIDs[uname] = id
		}
		base, err := url.Parse(cfg.PublicBaseURL)
		if err == nil && base.Hostname() != "" {
			fromDB, err := store.ListLocalActorsOnDomain(context.Background(), h.st.Pool, base.Hostname())
			if err != nil {
				return nil, fmt.Errorf("list local actors: %w", err)
			}
			seen := make(map[string]struct{})
			for _, u := range cfg.LocalUsernames {
				seen[u] = struct{}{}
			}
			for uname, id := range fromDB {
				if existing, ok := h.localActorIDs[uname]; ok && existing != id {
					return nil, fmt.Errorf("local actor %q: config id %d differs from database id %d", uname, existing, id)
				}
				h.localActorIDs[uname] = id
				if _, ok := seen[uname]; !ok {
					cfg.LocalUsernames = append(cfg.LocalUsernames, uname)
					seen[uname] = struct{}{}
				}
			}
		}
	}
	return h, nil
}

// IsLocalActor reports whether username is a known local account (configured or present in the database for this instance).
func (h *Handler) IsLocalActor(username string) bool {
	if h.localActorIDs != nil {
		id, ok := h.localActorIDs[username]
		return ok && id != 0
	}
	return h.cfg.IsLocalUsername(username)
}

// LocalActorID returns the database id for username when it is a local actor.
func (h *Handler) LocalActorID(username string) (int64, bool) {
	if h.localActorIDs == nil {
		return 0, false
	}
	id, ok := h.localActorIDs[username]
	return id, ok && id != 0
}

// FederationHTTPClient is the outbound HTTP client used for ActivityPub federation.
func (h *Handler) FederationHTTPClient() *http.Client { return h.fetchClient }

// FederationPolicy is the outbound fetch policy derived from configuration.
func (h *Handler) FederationPolicy() *fetch.Policy { return h.fetchPolicy }

// Config returns read-only service configuration for this handler.
func (h *Handler) Config() *config.Config { return h.cfg }

// WebFinger handles GET /.well-known/webfinger?resource=acct:user@host
func (h *Handler) WebFinger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("resource")
	if q == "" || !strings.HasPrefix(q, "acct:") {
		http.Error(w, "resource parameter required (acct:...)", http.StatusBadRequest)
		return
	}
	acct := strings.TrimPrefix(q, "acct:")
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured (AP_PUBLIC_BASE_URL)", http.StatusInternalServerError)
		return
	}
	base, err := url.Parse(h.cfg.PublicBaseURL)
	if err != nil {
		http.Error(w, "invalid AP_PUBLIC_BASE_URL", http.StatusInternalServerError)
		return
	}
	host := base.Hostname()
	user, domain, ok := strings.Cut(acct, "@")
	if !ok || user == "" || domain == "" {
		http.Error(w, "invalid acct", http.StatusBadRequest)
		return
	}
	if domain != host {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !h.IsLocalActor(user) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	subject := fmt.Sprintf("acct:%s@%s", user, host)
	profile := h.cfg.LocalActorProfileURL(user)
	resp := map[string]any{
		"subject": subject,
		"aliases": []string{profile},
		"links": []map[string]any{
			{
				"rel":  "self",
				"type": "application/activity+json",
				"href": profile,
			},
			{
				"rel":  "http://webfinger.net/rel/profile-page",
				"type": "text/html",
				"href": profile,
			},
		},
	}
	w.Header().Set("Content-Type", "application/jrd+json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// SharedInbox accepts signed ActivityPub POSTs (Digest + HTTP Signatures rsa-sha256 + remote keyId resolution).
func (h *Handler) SharedInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ct := r.Header.Get("Content-Type")
	if !isActivityJSONContentType(ct) {
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}
	max := h.inboxMaxBody
	if max <= 0 {
		max = 1 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if len(body) > max {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	sigHdr := r.Header.Get("Signature")
	if sigHdr == "" {
		http.Error(w, "missing Signature header", http.StatusUnauthorized)
		return
	}
	params, err := httpsig.ParseSignatureHeader(sigHdr)
	if err != nil {
		http.Error(w, "invalid Signature header", http.StatusUnauthorized)
		return
	}
	keyID := params["keyid"]
	if keyID == "" {
		http.Error(w, "missing keyId in signature", http.StatusUnauthorized)
		return
	}
	pub, err := fetch.PublicKeyForKeyID(r.Context(), h.fetchClient, h.fetchPolicy, keyID, h.cfg)
	if err != nil {
		http.Error(w, "could not resolve actor signing key", http.StatusUnauthorized)
		return
	}
	if err := httpsig.VerifyRequest(r, body, pub); err != nil {
		http.Error(w, "invalid digest or HTTP signature", http.StatusUnauthorized)
		return
	}

	rawFields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(body, &rawFields); err != nil {
		http.Error(w, "invalid activity json", http.StatusBadRequest)
		return
	}
	activityID, err := jsonStringField(rawFields, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	activityType, err := activityTypeString(rawFields)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actorIRI, err := actorIRIFromActivity(rawFields)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !signingActorMatchesKeyID(keyID, actorIRI) {
		http.Error(w, "signature actor does not match activity actor", http.StatusUnauthorized)
		return
	}

	if h.st == nil || h.q == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	signerPEM, err := actorkey.PublicKeyPEMFromRSA(pub)
	if err != nil {
		http.Error(w, "internal key encoding", http.StatusInternalServerError)
		return
	}
	remoteID, err := store.EnsureRemoteActor(r.Context(), h.st.Pool, actorIRI, signerPEM)
	if err != nil {
		http.Error(w, "persist actor", http.StatusInternalServerError)
		return
	}

	if sqlQ, ok := h.q.(*sqlqueue.SQL); ok {
		tx, err := h.st.Pool.Begin(r.Context())
		if err != nil {
			http.Error(w, "tx begin", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())

		inserted, actDBID, err := store.InsertInboundActivity(r.Context(), tx, remoteID, activityID, activityType, body)
		if err != nil {
			http.Error(w, "persist activity", http.StatusInternalServerError)
			return
		}
		if !inserted {
			if err := tx.Commit(r.Context()); err != nil {
				http.Error(w, "tx commit", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		payload, err := json.Marshal(map[string]int64{"activityDbId": actDBID})
		if err != nil {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		job := queue.Job{
			Type:           queue.TypeProcessInboxActivity,
			Payload:        payload,
			IdempotencyKey: activityID,
		}
		if err := sqlQ.EnqueueTx(r.Context(), tx, job); err != nil {
			http.Error(w, "enqueue job", http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, "tx commit", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	inserted, actDBID, err := store.InsertInboundActivity(r.Context(), h.st.Pool, remoteID, activityID, activityType, body)
	if err != nil {
		http.Error(w, "persist activity", http.StatusInternalServerError)
		return
	}
	if !inserted {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	payload, err := json.Marshal(map[string]int64{"activityDbId": actDBID})
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	job := queue.Job{
		Type:           queue.TypeProcessInboxActivity,
		Payload:        payload,
		IdempotencyKey: activityID,
	}
	if err := h.q.Enqueue(r.Context(), job); err != nil {
		http.Error(w, "enqueue job", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func isActivityJSONContentType(ct string) bool {
	ct = strings.TrimSpace(ct)
	if ct == "" {
		return false
	}
	base, _, _ := strings.Cut(ct, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	return base == "application/activity+json" || base == "application/ld+json"
}

func jsonStringField(m map[string]json.RawMessage, key string) (string, error) {
	raw, ok := m[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("invalid or empty %s", key)
	}
	return s, nil
}

func activityTypeString(m map[string]json.RawMessage) (string, error) {
	raw, ok := m["type"]
	if !ok {
		return "", fmt.Errorf("missing type")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return s, nil
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		if s, ok := arr[0].(string); ok && s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("invalid type field")
}

func actorIRIFromActivity(m map[string]json.RawMessage) (string, error) {
	raw, ok := m["actor"]
	if !ok {
		return "", fmt.Errorf("missing actor")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return s, nil
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.ID != "" {
		return obj.ID, nil
	}
	return "", fmt.Errorf("actor must be an id string or an object with id")
}

func signingActorMatchesKeyID(keyID, actorIRI string) bool {
	k, err := url.Parse(keyID)
	if err != nil {
		return false
	}
	a, err := url.Parse(actorIRI)
	if err != nil {
		return false
	}
	k.Fragment, a.Fragment = "", ""
	return strings.TrimRight(k.String(), "/") == strings.TrimRight(a.String(), "/")
}

func (h *Handler) primaryLocalUsername() string {
	if len(h.cfg.LocalUsernames) > 0 {
		return h.cfg.LocalUsernames[0]
	}
	return h.cfg.LocalUsername
}

// GetRoot handles GET / so the public origin is not a 404. Typical browsers get a minimal HTML page; JSON-oriented Accept headers are redirected to the primary local actor document.
func (h *Handler) GetRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if strings.TrimSpace(h.cfg.PublicBaseURL) == "" {
		http.Error(w, "not configured", http.StatusServiceUnavailable)
		return
	}
	user := h.primaryLocalUsername()
	if strings.TrimSpace(user) == "" {
		http.NotFound(w, r)
		return
	}
	prof := h.cfg.LocalActorProfileURL(user)
	acc := strings.ToLower(r.Header.Get("Accept"))
	wantHTML := acc == "" ||
		(strings.Contains(acc, "text/html") &&
			!strings.Contains(acc, "application/json") &&
			!strings.Contains(acc, "application/activity+json") &&
			!strings.Contains(acc, "application/ld+json"))
	if wantHTML {
		host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(h.cfg.PublicBaseURL), "https://"), "http://")
		host = strings.TrimSuffix(host, "/")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title>`+
			`<p><strong>%s</strong> — ActivityPub (activitypub-core)</p>`+
			`<ul><li><a href="%s">Local profile (@%s)</a></li>`+
			`<li><a href="/.well-known/actor">Instance actor</a></li></ul>`,
			host, host, prof, html.EscapeString(user))
		return
	}
	http.Redirect(w, r, prof, http.StatusFound)
}

// Mount registers routes on mux. basePath is typically empty (Host root).
func (h *Handler) Mount(mux *http.ServeMux) {
	// GET / is handled via GetActivityOrObject → GetRoot (cannot register both GET / and GET /{path...} on Go 1.22+ ServeMux).
	mux.HandleFunc("GET /.well-known/webfinger", h.WebFinger)
	mux.HandleFunc("GET /.well-known/actor", h.GetInstanceActor)
	mux.HandleFunc("GET /actor", h.RedirectInstanceActorAlias)

	mux.HandleFunc("POST /media", h.PostMediaUpload)
	mux.HandleFunc("GET /media/{key...}", h.GetMedia)

	mux.HandleFunc("POST /inbox", h.SharedInbox)

	// Resolve any path to persisted activity_id / object_url (local actors only). Must stay last among GET routes in this mux.
	mux.HandleFunc("GET /{path...}", h.GetActivityOrObject)
}

// Health is a no-op liveness handler.
func Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Ready pings the database when store is non-nil.
func Ready(store interface{ Ping(context.Context) error }) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if err := store.Ping(r.Context()); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
