package api

import (
	"net/http"
	"strconv"

	"github.com/user/uea/internal/store"
)

// registerErrorRoutes wires the error log onto an authenticated mux.
//
// Not under /api/import even though import is the only producer today: the log
// is categorised, and moving the URL later would break whatever is reading it.
func registerErrorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/errors", handleErrorList)
	mux.HandleFunc("DELETE /api/errors", handleErrorClear)
}

func errorQueryFrom(r *http.Request) store.ErrorQuery {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return store.ErrorQuery{
		Category: r.URL.Query().Get("category"),
		JobID:    r.URL.Query().Get("jobId"),
		Limit:    limit,
		Offset:   offset,
	}
}

func handleErrorList(w http.ResponseWriter, r *http.Request) {
	q := errorQueryFrom(r)

	entries, err := store.ListErrors(q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if entries == nil {
		entries = []*store.LoggedError{}
	}

	// The total is separate from the page so the UI can say "showing 200 of
	// 4,312" rather than implying the page is everything.
	total, err := store.CountErrors(store.ErrorQuery{Category: q.Category, JobID: q.JobID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":   total,
		"count":   len(entries),
		"entries": entries,
	})
}

func handleErrorClear(w http.ResponseWriter, r *http.Request) {
	removed, err := store.ClearErrors(errorQueryFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": removed})
}
