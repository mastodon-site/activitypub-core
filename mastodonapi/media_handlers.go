package mastodonapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mastodon-site/activitypub-core/store"
)

var errMissingMultipartBoundary = errors.New("missing multipart boundary")

func mastodonMediaTypeFromMIME(m string) string {
	if strings.HasPrefix(strings.ToLower(m), "image/") {
		return "image"
	}
	return "unknown"
}

func parseBoolish(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

func mimeAllowed(cfgMime []string, fileCT string) bool {
	fileCT = strings.TrimSpace(strings.ToLower(fileCT))
	if fileCT == "" {
		return false
	}
	for _, a := range cfgMime {
		if fileCT == strings.TrimSpace(strings.ToLower(a)) {
			return true
		}
	}
	return false
}

func inferMIMEFromFilename(name string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(name)))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

// effectiveUploadContentType picks a concrete MIME when clients send multipart file
// parts as application/octet-stream (Go's multipart.Writer.CreateFormFile does this)
// but supply a useful filename (e.g. photo.png).
func effectiveUploadContentType(declared, filename string) string {
	d := strings.TrimSpace(strings.ToLower(declared))
	if d != "" && d != "application/octet-stream" {
		return strings.TrimSpace(declared)
	}
	if inf := inferMIMEFromFilename(filename); inf != "" {
		return inf
	}
	if strings.TrimSpace(declared) == "" {
		return "application/octet-stream"
	}
	return strings.TrimSpace(declared)
}

type parsedMediaUpload struct {
	FileBody        []byte
	FileContentType string
	FileName        string
	Description     string
	Sensitive       bool
}

func (s *Server) readMediaUpload(r *http.Request, max int) (*parsedMediaUpload, error) {
	ct := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, err
	}
	out := &parsedMediaUpload{}
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary, ok := params["boundary"]
		if !ok {
			return nil, errMissingMultipartBoundary
		}
		mr := multipart.NewReader(io.LimitReader(r.Body, int64(max)+1), boundary)
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			name := p.FormName()
			switch name {
			case "file":
				out.FileContentType = p.Header.Get("Content-Type")
				if out.FileContentType == "" {
					out.FileContentType = "application/octet-stream"
				}
				// Use multipart.Part's parser (same as FormName); filepath.Base applied per RFC 7578.
				if fn := p.FileName(); fn != "" {
					out.FileName = fn
				} else if cd := strings.TrimSpace(p.Header.Get("Content-Disposition")); cd != "" {
					_, dispParams, derr := mime.ParseMediaType(cd)
					if derr == nil {
						out.FileName = dispParams["filename"]
					}
				}
				out.FileBody, err = io.ReadAll(io.LimitReader(p, int64(max)+1))
				_ = p.Close()
				if err != nil {
					return nil, err
				}
			case "description":
				b, rerr := io.ReadAll(io.LimitReader(p, 1<<16))
				_ = p.Close()
				if rerr != nil {
					return nil, rerr
				}
				out.Description = strings.TrimSpace(string(b))
			case "sensitive":
				b, rerr := io.ReadAll(io.LimitReader(p, 256))
				_ = p.Close()
				if rerr != nil {
					return nil, rerr
				}
				out.Sensitive = parseBoolish(string(b))
			default:
				_, _ = io.Copy(io.Discard, p)
				_ = p.Close()
			}
		}
	default:
		out.FileContentType = ct
		if out.FileContentType == "" {
			out.FileContentType = "application/octet-stream"
		}
		var err error
		out.FileBody, err = io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Server) mastodonMediaAttachmentMap(mr *store.MastodonMediaRow) map[string]any {
	base := strings.TrimRight(s.cfg().PublicBaseURL, "/")
	var url any
	var preview any
	if mr.ProcessingState == store.MediaProcessingComplete {
		u := base + "/media/" + mr.BlobKey
		url = u
		preview = u
	} else {
		url = nil
		preview = nil
	}
	desc := mr.Description
	var descOut any
	if desc != "" {
		descOut = desc
	} else {
		descOut = nil
	}
	return map[string]any{
		"id":                 strconv.FormatInt(mr.ID, 10),
		"type":               mastodonMediaTypeFromMIME(mr.ContentType),
		"url":                url,
		"preview_url":        preview,
		"remote_url":         nil,
		"preview_remote_url": nil,
		"text_url":           nil,
		"meta":               map[string]any{},
		"description":        descOut,
		"blurhash":           nil,
		"sensitive":          mr.Sensitive,
	}
}

func (s *Server) writeMediaAttachment(w http.ResponseWriter, mr *store.MastodonMediaRow, httpStatus int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(s.mastodonMediaAttachmentMap(mr))
}

func (s *Server) postAPIMedia(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.handleMediaPost(w, r, actorID, false)
}

func (s *Server) postAPIMediaV2(w http.ResponseWriter, r *http.Request, actorID int64) {
	s.handleMediaPost(w, r, actorID, true)
}

// handleMediaPost implements POST /api/v1/media (blocking) and /api/v2/media (may return 202 when AP_MEDIA_ASYNC_UPLOAD is set).
func (s *Server) handleMediaPost(w http.ResponseWriter, r *http.Request, actorID int64, v2 bool) {
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
	allowed := s.cfg().EffectiveMediaAllowedMIMETypes()

	parsed, err := s.readMediaUpload(r, max)
	if err != nil {
		if errors.Is(err, errMissingMultipartBoundary) {
			writeAPIError(w, http.StatusBadRequest, "missing boundary")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid content type")
		return
	}
	if len(parsed.FileBody) == 0 {
		writeAPIError(w, http.StatusBadRequest, "empty upload")
		return
	}
	if len(parsed.FileBody) > max {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	fileCT := effectiveUploadContentType(parsed.FileContentType, parsed.FileName)
	if !mimeAllowed(allowed, fileCT) {
		writeAPIError(w, http.StatusUnprocessableEntity, "Validation failed: File content type is invalid, File is invalid")
		return
	}

	key, err := s.H.StoreBlob(r.Context(), fileCT, parsed.FileBody)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "media storage not available")
		return
	}

	async := v2 && s.cfg().MediaAsyncUpload
	initialState := store.MediaProcessingComplete
	if async {
		initialState = store.MediaProcessingPending
	}

	mid, err := store.InsertMastodonMedia(r.Context(), s.Pool, actorID, key, fileCT, int64(len(parsed.FileBody)), parsed.Description, parsed.Sensitive, initialState)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not record media")
		return
	}

	if async {
		pool := s.Pool
		go func(mediaID int64) {
			ctx := context.Background()
			_ = store.SetMastodonMediaProcessingState(ctx, pool, mediaID, store.MediaProcessingComplete)
		}(mid)
		mr := &store.MastodonMediaRow{
			ID:              mid,
			ActorID:         actorID,
			BlobKey:         key,
			ContentType:     fileCT,
			ByteSize:        int64(len(parsed.FileBody)),
			Description:     parsed.Description,
			Sensitive:       parsed.Sensitive,
			ProcessingState: store.MediaProcessingPending,
		}
		s.writeMediaAttachment(w, mr, http.StatusAccepted)
		return
	}

	mr, err := store.GetMastodonMediaForActor(r.Context(), s.Pool, mid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not load media")
		return
	}
	s.writeMediaAttachment(w, mr, http.StatusOK)
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
		Description *string `json:"description"`
		Sensitive   *bool   `json:"sensitive"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Description == nil && body.Sensitive == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpdateMastodonMediaMetadata(r.Context(), s.Pool, mid, actorID, body.Description, body.Sensitive); err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	mr, err := store.GetMastodonMediaForActor(r.Context(), s.Pool, mid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	s.writeMediaAttachment(w, mr, http.StatusOK)
}

func (s *Server) getAPIMedia(w http.ResponseWriter, r *http.Request, actorID int64) {
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
	mr, err := store.GetMastodonMediaForActor(r.Context(), s.Pool, mid, actorID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "Record not found")
		return
	}
	switch mr.ProcessingState {
	case store.MediaProcessingFailed:
		writeAPIError(w, http.StatusUnprocessableEntity, "Validation failed: File content type is invalid, File is invalid")
		return
	case store.MediaProcessingPending:
		s.writeMediaAttachment(w, mr, http.StatusPartialContent)
		return
	default:
		s.writeMediaAttachment(w, mr, http.StatusOK)
	}
}
