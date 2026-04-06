package aphttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// PostMediaUpload accepts raw body storage at POST /media (Bearer AP_MEDIA_UPLOAD_SECRET).
func (h *Handler) PostMediaUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.blobs == nil || strings.TrimSpace(h.cfg.MediaUploadSecret) == "" {
		http.Error(w, "media upload not configured", http.StatusServiceUnavailable)
		return
	}
	if !mediaBearerAuthorized(r, h.cfg.MediaUploadSecret) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	max := h.cfg.MediaMaxUploadBytes
	if max <= 0 {
		max = 10 << 20
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
	ct := strings.TrimSpace(r.Header.Get("Content-Type"))
	if ct == "" {
		ct = "application/octet-stream"
	}
	key, err := randomHexKey(16)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if err := h.blobs.Put(r.Context(), key, ct, bytes.NewReader(body), int64(len(body))); err != nil {
		http.Error(w, "store blob", http.StatusInternalServerError)
		return
	}
	base := strings.TrimRight(h.cfg.PublicBaseURL, "/")
	url := base + "/media/" + key
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"url": url, "mediaType": ct})
}

// GetMedia serves GET /media/{key...}
func (h *Handler) GetMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.blobs == nil {
		http.NotFound(w, r)
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	key = strings.Trim(key, "/")
	if !safeMediaKey(key) {
		http.NotFound(w, r)
		return
	}
	rc, ct, err := h.blobs.Get(r.Context(), key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "media", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	_, _ = io.Copy(w, rc)
}

func mediaBearerAuthorized(r *http.Request, secret string) bool {
	return outboxBearerAuthorized(r, secret)
}

func randomHexKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// StoreBlob writes bytes to the configured blob store under a random key (for Mastodon API uploads).
func (h *Handler) StoreBlob(ctx context.Context, contentType string, data []byte) (key string, err error) {
	if h.blobs == nil {
		return "", fmt.Errorf("blob store not configured")
	}
	if len(data) == 0 {
		return "", fmt.Errorf("empty body")
	}
	ct := strings.TrimSpace(contentType)
	if ct == "" {
		ct = "application/octet-stream"
	}
	key, err = randomHexKey(16)
	if err != nil {
		return "", err
	}
	if err := h.blobs.Put(ctx, key, ct, bytes.NewReader(data), int64(len(data))); err != nil {
		return "", err
	}
	return key, nil
}

func safeMediaKey(k string) bool {
	if k == "" || strings.Contains(k, "..") {
		return false
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '/', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
