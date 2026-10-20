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
	// The screenshots the guides show. They change only with the binary, so
	// a browser may keep them for a day.
	mux.HandleFunc("GET /api/docs/images/{name}", func(w http.ResponseWriter, r *http.Request) {
		b, err := docs.Image(r.PathValue("name"))
		if err != nil {
			writeError(w, http.StatusNotFound, "no image %q", r.PathValue("name"))
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("Cache-Control", "max-age=86400")
		_, _ = w.Write(b)
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
