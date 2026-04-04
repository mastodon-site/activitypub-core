package aphttp

import "net/http"

// testMounted returns the HTTP handler the apd binary effectively serves (mux + legacy redirects).
func testMounted(h *Handler) http.Handler {
	m := http.NewServeMux()
	h.Mount(m)
	return h.WithLegacy(h.WithAtPaths(m))
}
