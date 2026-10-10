package api

import (
	"net/http"

	"github.com/user/inboxql/internal/docs"
)

// The guides, as the Help menu reads them. Embedded in the binary, so they
// describe the version that is running and work without a network.
func registerDocsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"guides": docs.List(), "version": currentVersionInfo.Version})
	})
	// The whole index in one response: about a hundred kilobytes, fetched
	// once, so search answers on every keystroke without a round trip.
	mux.HandleFunc("GET /api/docs/index", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		writeJSON(w, http.StatusOK, map[string]any{"sections": docs.Index()})
	})
	mux.HandleFunc("GET /api/docs/{slug}", func(w http.ResponseWriter, r *http.Request) {
		page, err := docs.Render(r.PathValue("slug"), docs.AppLinks)
		if err != nil {
			writeError(w, http.StatusNotFound, "%v", err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
}
