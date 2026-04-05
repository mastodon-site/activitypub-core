package mastodonapi

import "net/http"

func (s *Server) v1StubGETEmptyArray(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) v1StubBearerEmptyArray(w http.ResponseWriter, r *http.Request, _ int64) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONArrayOK(w, nil)
}

func (s *Server) v1StubBearerEmptyObject(w http.ResponseWriter, r *http.Request, _ int64) {
	writeJSONObjectOK(w, map[string]any{})
}

func (s *Server) v1ForbiddenAdmin(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, http.StatusForbidden, "This action is outside the authorized scope")
}
