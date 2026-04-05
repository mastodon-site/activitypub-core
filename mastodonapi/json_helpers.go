package mastodonapi

import (
	"encoding/json"
	"net/http"
)

func writeJSONResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONObjectOK(w http.ResponseWriter, v map[string]any) {
	if v == nil {
		v = map[string]any{}
	}
	writeJSONResponse(w, http.StatusOK, v)
}

func writeJSONArrayOK(w http.ResponseWriter, v []any) {
	if v == nil {
		v = []any{}
	}
	writeJSONResponse(w, http.StatusOK, v)
}
