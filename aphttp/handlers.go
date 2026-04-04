// Package aphttp serves ActivityPub HTTP surfaces (WebFinger, actors, inboxes).
package aphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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
}

// Handler bundles AP HTTP handlers for mounting on the API mux.
type Handler struct {
	cfg *config.Config
	// actorPublicKeyPEM is PKIX PEM for JSON-LD publicKey.publicKeyPem; empty means use stub in GetActor.
	actorPublicKeyPEM string
	fetchClient       *http.Client
	inboxMaxBody      int
	st                *store.Postgres
	q                 queue.Backend
	localActorIDs     map[string]int64 // username -> actors.id
}

// New creates AP HTTP handlers. cfg.PublicBaseURL must be set for meaningful responses.
// If cfg sets actor key paths, the PEM for the actor document is loaded at startup.
// When deps.Store is set, each configured local username is upserted into actors and localActorIDs is populated.
func New(cfg *config.Config, deps Deps) (*Handler, error) {
	h := &Handler{
		cfg: cfg,
		st:  deps.Store,
		q:   deps.Queue,
		fetchClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		},
		inboxMaxBody: cfg.InboxMaxBody,
	}
	if cfg.ActorPrivateKeyPath != "" {
		pub, err := actorkey.ActorPublicKeyPEMForConfig(cfg)
		if err != nil {
			return nil, err
		}
		h.actorPublicKeyPEM = pub
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
	}
	return h, nil
}

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
	if !h.cfg.IsLocalUsername(user) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	subject := fmt.Sprintf("acct:%s@%s", user, host)
	profile := strings.TrimRight(h.cfg.PublicBaseURL, "/") + "/users/" + url.PathEscape(user)
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

// GetActor returns a minimal Actor for the local user (stub until DB-backed).
func (h *Handler) GetActor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" {
		http.Error(w, "server not configured", http.StatusInternalServerError)
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/users/")
	username = strings.Trim(username, "/")
	if !h.cfg.IsLocalUsername(username) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	base := strings.TrimRight(h.cfg.PublicBaseURL, "/")
	profile := base + "/users/" + url.PathEscape(username)
	inbox := base + "/inbox"
	outbox := base + "/outbox/" + url.PathEscape(username)
	keyID := profile + "#main-key"
	publicPEM := h.actorPublicKeyPEM
	if publicPEM == "" {
		publicPEM = "-----BEGIN PUBLIC KEY-----\n(stub — set AP_ACTOR_PRIVATE_KEY_PATH)\n-----END PUBLIC KEY-----\n"
	}
	actor := map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                profile,
		"type":              "Person",
		"preferredUsername": username,
		"inbox":             inbox,
		"outbox":            outbox,
		"publicKey": map[string]any{
			"id":           keyID,
			"owner":        profile,
			"type":         "Key",
			"publicKeyPem": publicPEM,
		},
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/ld+json") || strings.Contains(accept, "application/activity+json") {
		w.Header().Set("Content-Type", "application/activity+json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"; charset=utf-8")
	}
	_ = json.NewEncoder(w).Encode(actor)
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
	pub, err := fetch.PublicKeyForKeyID(r.Context(), h.fetchClient, keyID)
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

// GetOutbox returns an OrderedCollection of outbound activity IRIs for the local user (newest first).
func (h *Handler) GetOutbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.PublicBaseURL == "" || h.st == nil || len(h.localActorIDs) == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	username := r.PathValue("username")
	if username == "" || !h.cfg.IsLocalUsername(username) {
		http.NotFound(w, r)
		return
	}
	actorID, ok := h.localActorIDs[username]
	if !ok || actorID == 0 {
		http.Error(w, "server not configured", http.StatusServiceUnavailable)
		return
	}
	total, items, err := store.OutboxPage(r.Context(), h.st.Pool, actorID, 50)
	if err != nil {
		http.Error(w, "outbox", http.StatusInternalServerError)
		return
	}
	base := strings.TrimRight(h.cfg.PublicBaseURL, "/")
	collID := base + "/outbox/" + url.PathEscape(username)
	doc := map[string]any{
		"@context":     "https://www.w3.org/ns/activitystreams",
		"id":           collID,
		"type":         "OrderedCollection",
		"totalItems":   total,
		"orderedItems": items,
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/activity+json") || strings.Contains(accept, "application/ld+json") {
		w.Header().Set("Content-Type", "application/activity+json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"; charset=utf-8")
	}
	_ = json.NewEncoder(w).Encode(doc)
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

// Mount registers routes on mux. basePath is typically empty (Host root).
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/webfinger", h.WebFinger)
	mux.HandleFunc("GET /users/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Count(strings.TrimSuffix(r.URL.Path, "/"), "/") > 1 {
			http.NotFound(w, r)
			return
		}
		h.GetActor(w, r)
	})
	mux.HandleFunc("GET /outbox/{username}", h.GetOutbox)
	mux.HandleFunc("POST /outbox/{username}", h.PostOutbox)
	mux.HandleFunc("POST /inbox", h.SharedInbox)
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
