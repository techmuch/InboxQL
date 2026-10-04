package api

import (
	"net/http"

	"github.com/user/inboxql/internal/store"
)

// # Arranging the rail, over HTTP
//
// The rail was three lists with three natures written inline in a component.
// These are the operations that change it, and they are deliberately separate
// from saving a query: editing what a query *is* should not move it, and moving
// it should not touch its text.

func registerRailRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rail/defaults", handleRailDefaults)
	mux.HandleFunc("POST /api/rail/defaults", handleRailInstall)
	mux.HandleFunc("GET /api/rail/folders", handleRailFolders)
	mux.HandleFunc("PUT /api/rail/folders", handleRailSetFolder)
	mux.HandleFunc("POST /api/rail/folders/reset", handleRailResetFolders)
	mux.HandleFunc("PUT /api/rail/order", handleRailMove)
	mux.HandleFunc("PUT /api/rail/icon", handleRailIcon)
}

// handleRailDefaults lists the pack and what each would match here.
func handleRailDefaults(w http.ResponseWriter, _ *http.Request) {
	existing := map[string]bool{}
	if all, err := store.ListSavedQueries(); err == nil {
		for _, q := range all {
			existing[q.Name] = true
		}
	}

	type row struct {
		store.RailDefault
		// Reach is how many rows it would match. Zero is a real answer and the
		// useful one: an entry that finds nothing is a row that teaches
		// somebody the feature does not work.
		Reach   int64 `json:"reach"`
		Present bool  `json:"present"`
	}
	out := make([]row, 0, len(store.RailDefaults))
	for _, d := range store.RailDefaults {
		reach, _ := store.RailDefaultReach(d)
		out = append(out, row{RailDefault: d, Reach: reach, Present: existing[d.Name]})
	}
	writeJSON(w, http.StatusOK, out)
}

func handleRailInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names []string `json:"names"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	added, skipped, err := store.InstallRailDefaults(req.Names)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "skipped": skipped})
}

func handleRailFolders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"folders": store.RailFolders,
		"hidden":  store.HiddenFolders(),
		"icons":   store.RailIcons,
	})
}

func handleRailSetFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder string `json:"folder"`
		Hidden *bool  `json:"hidden"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if req.Hidden == nil {
		// A bool's zero value is false, so an absent field would read as
		// "show it" — a request that said nothing would un-hide something.
		writeError(w, http.StatusBadRequest, `send {"folder":"spam","hidden":true}`)
		return
	}
	if err := store.SetFolderHidden(req.Folder, *req.Hidden); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hidden": store.HiddenFolders()})
}

// handleRailResetFolders is the way back.
func handleRailResetFolders(w http.ResponseWriter, _ *http.Request) {
	if err := store.ShowAllFolders(); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hidden": []string{}})
}

func handleRailMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		To   *int   `json:"to"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if req.To == nil {
		writeError(w, http.StatusBadRequest, `send {"name":"files","to":0}`)
		return
	}
	if err := store.MoveSavedQuery(req.Name, *req.To); err != nil {
		writeError(w, http.StatusNotFound, "%v", err)
		return
	}
	queries, err := store.ListSavedQueries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queries": queries})
}

func handleRailIcon(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Icon string `json:"icon"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if err := store.SetSavedQueryIcon(req.Name, req.Icon); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
