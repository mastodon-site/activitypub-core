package mastodonapi

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

func (s *Server) postAPIMedia(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	max := s.cfg().MediaMaxUploadBytes
	if max <= 0 {
		max = 10 << 20
	}
	ct := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid content type")
		return
	}
	var fileBody []byte
	var fileContentType string
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary, ok := params["boundary"]
		if !ok {
			writeAPIError(w, http.StatusBadRequest, "missing boundary")
			return
		}
		mr := multipart.NewReader(io.LimitReader(r.Body, int64(max)+1), boundary)
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			if p.FormName() != "file" {
				_, _ = io.Copy(io.Discard, p)
				continue
			}
			fileContentType = p.Header.Get("Content-Type")
			if fileContentType == "" {
				fileContentType = "application/octet-stream"
			}
			fileBody, err = io.ReadAll(io.LimitReader(p, int64(max)+1))
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, "could not read upload")
				return
			}
			break
		}
	default:
		fileContentType = ct
		if fileContentType == "" {
			fileContentType = "application/octet-stream"
		}
		fileBody, err = io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "could not read body")
			return
		}
	}
	if len(fileBody) == 0 {
		writeAPIError(w, http.StatusBadRequest, "empty upload")
		return
	}
	if len(fileBody) > max {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	key, err := s.H.StoreBlob(r.Context(), fileContentType, fileBody)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "media storage not available")
		return
	}
	mid, err := store.InsertMastodonMedia(r.Context(), s.Pool, actorID, key, fileContentType, int64(len(fileBody)))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not record media")
		return
	}
	base := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	u := base + "/media/" + key
	writeJSONObjectOK(w, map[string]any{
		"id":                 strconv.FormatInt(mid, 10),
		"type":               mastodonMediaTypeFromMIME(fileContentType),
		"url":                u,
		"preview_url":        u,
		"remote_url":         nil,
		"preview_remote_url": nil,
		"text_url":           nil,
		"meta":               map[string]any{},
		"description":        nil,
		"blurhash":           nil,
	})
}

func mastodonMediaTypeFromMIME(m string) string {
	if strings.HasPrefix(strings.ToLower(m), "image/") {
		return "image"
	}
	return "unknown"
}

func (s *Server) putAPIMedia(w http.ResponseWriter, r *http.Request, actorID int64) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusInternalServerError, "database not configured")
		return
	}
	raw := r.PathValue("id")
	mid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || mid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	var body struct {
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpdateMastodonMediaDescription(r.Context(), s.Pool, mid, actorID, body.Description); err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	mr, err := store.GetMastodonMediaForActor(r.Context(), s.Pool, mid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	base := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	u := base + "/media/" + mr.BlobKey
	writeJSONObjectOK(w, map[string]any{
		"id":                 strconv.FormatInt(mr.ID, 10),
		"type":               mastodonMediaTypeFromMIME(mr.ContentType),
		"url":                u,
		"preview_url":        u,
		"remote_url":         nil,
		"preview_remote_url": nil,
		"text_url":           nil,
		"meta":               map[string]any{},
		"description":        body.Description,
		"blurhash":           nil,
	})
}

func (s *Server) getAPIMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Pool == nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	raw := r.PathValue("id")
	mid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || mid < 1 {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	mr, err := store.GetMastodonMediaByID(r.Context(), s.Pool, mid)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	base := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	u := base + "/media/" + mr.BlobKey
	writeJSONObjectOK(w, map[string]any{
		"id":                 strconv.FormatInt(mr.ID, 10),
		"type":               mastodonMediaTypeFromMIME(mr.ContentType),
		"url":                u,
		"preview_url":        u,
		"remote_url":         nil,
		"preview_remote_url": nil,
		"text_url":           nil,
		"meta":               map[string]any{},
		"description":        nil,
		"blurhash":           nil,
	})
}
