package api

import (
	"net/http"

	"github.com/user/inboxql/internal/store"
)

// # Important dates, over HTTP
//
// The list, adding or changing one, and removing one. See
// store/importantdates.go for what a date is and who may mark one.

func registerDateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dates", handleDatesList)
	mux.HandleFunc("POST /api/dates", handleDateSave)
	mux.HandleFunc("DELETE /api/dates/{id}", handleDateDelete)
	mux.HandleFunc("GET /api/topics", handleTopics)
}

// handleTopics lists the topics in use, most used first, for topic pages.
func handleTopics(w http.ResponseWriter, r *http.Request) {
	limit, _ := clampPaging(r, 100, 500)
	ts, err := store.TopicCounts(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": ts})
}

func handleDatesList(w http.ResponseWriter, _ *http.Request) {
	ds, err := store.ListImportantDates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dates": ds})
}

func handleDateSave(w http.ResponseWriter, r *http.Request) {
	var d store.ImportantDate
	if decodeJSON(w, r, &d) != nil {
		return
	}
	if err := store.SaveImportantDate(&d); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func handleDateDelete(w http.ResponseWriter, r *http.Request) {
	if err := store.DeleteImportantDate(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": r.PathValue("id")})
}
