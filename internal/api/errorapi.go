package api

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/user/inboxql/internal/logging"
	"github.com/user/inboxql/internal/store"
)

// registerErrorRoutes wires the error log onto an authenticated mux.
//
// Not under /api/import even though import is the only producer today: the log
// is categorised, and moving the URL later would break whatever is reading it.
func registerErrorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/errors", handleErrorList)
	mux.HandleFunc("DELETE /api/errors", handleErrorClear)
	mux.HandleFunc("GET /api/log/level", handleLogLevelGet)
	mux.HandleFunc("PUT /api/log/level", handleLogLevelSet)
}

func errorQueryFrom(r *http.Request) store.ErrorQuery {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return store.ErrorQuery{
		Category: r.URL.Query().Get("category"),
		JobID:    r.URL.Query().Get("jobId"),
		MinLevel: r.URL.Query().Get("level"),
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
	total, err := store.CountErrors(store.ErrorQuery{
		Category: q.Category, JobID: q.JobID, MinLevel: q.MinLevel})
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

// handleLogLevelGet reports the floor, and what it means.
func handleLogLevelGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"level":   logging.Level(),
		"levels":  store.Levels,
		"dropped": logging.Dropped(),
	})
}

// handleLogLevelSet changes the floor, now and for the next run.
//
// Both: the running process is the one somebody is watching, and the stored
// value is what makes the change outlive a restart. Setting only one of them
// is the version that reads as broken — either it does nothing until a restart
// or it forgets when you do.
func handleLogLevelSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if !slices.Contains(store.Levels, strings.ToLower(req.Level)) {
		writeError(w, http.StatusBadRequest,
			"%q is not a level (%s)", req.Level, strings.Join(store.Levels, ", "))
		return
	}
	logging.SetLevel(req.Level)
	if err := store.UpdateSetting("log.level", strings.ToLower(req.Level)); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"level": logging.Level()})
}
