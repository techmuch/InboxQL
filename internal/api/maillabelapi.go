package api

import (
	"net/http"
	"strings"

	"github.com/user/inboxql/internal/store"
)

// # Mail labels over HTTP
//
// The labels a person and the server put on messages — inbox, trash, the
// user's own — as opposed to annotator verdicts, which labelapi.go serves.
// Every change here is applied locally at once and queued for the server.

func registerMailLabelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/labels", handleLabelList)
	mux.HandleFunc("POST /api/labels", handleLabelCreate)
	mux.HandleFunc("DELETE /api/labels/{name...}", handleLabelDelete)
}

// registerMessageLabelRoutes shares the /api/messages/ prefix, so it is added
// to that mux.
func registerMessageLabelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/messages/labels", handleChangeLabels)
	mux.HandleFunc("POST /api/messages/move", handleMove)
	mux.HandleFunc("GET /api/messages/bulk/count", handleBulkCount)
	mux.HandleFunc("POST /api/messages/bulk", handleBulk)
	mux.HandleFunc("POST /api/messages/bulk/undo", handleBulkUndo)
}

// maxBatch bounds one request, so it cannot rewrite the whole mailbox in one
// transaction.
const maxBatch = 1000

func handleLabelList(w http.ResponseWriter, r *http.Request) {
	counts, err := store.LabelCounts(r.URL.Query().Get("account"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if counts == nil {
		counts = []store.LabelCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": counts})
}

func handleLabelCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if err := store.CreateLabel(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": store.NormalizeLabel(req.Name)})
}

func handleLabelDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !store.LabelExists(name) {
		writeError(w, http.StatusNotFound, "no label named %q", name)
		return
	}
	n, err := store.DeleteLabel(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": name, "messages": n})
}

func handleChangeLabels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []string `json:"ids"`
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
		Origin string   `json:"origin"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if !checkBatch(w, req.IDs) {
		return
	}
	n, err := store.ChangeLabels(req.IDs, store.LabelChange{Add: req.Add, Remove: req.Remove, Source: origin(req.Origin)})
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": n})
}

func handleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []string `json:"ids"`
		To     string   `json:"to"`
		Origin string   `json:"origin"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if !checkBatch(w, req.IDs) {
		return
	}
	move, ok := store.Moves[strings.ToLower(req.To)]
	if !ok {
		writeError(w, http.StatusBadRequest, "%q is not a move (archive, inbox, trash, untrash, spam, notspam)", req.To)
		return
	}
	move.Source = origin(req.Origin)
	n, err := store.ChangeLabels(req.IDs, move)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": n})
}

func checkBatch(w http.ResponseWriter, ids []string) bool {
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "no messages given")
		return false
	}
	if len(ids) > maxBatch {
		writeError(w, http.StatusBadRequest, "too many messages in one request (%d); %d at a time", len(ids), maxBatch)
		return false
	}
	return true
}

func origin(o string) string {
	if strings.EqualFold(o, "agent") {
		return store.LabelSourceAgent
	}
	return store.LabelSourceHuman
}

// handleBulkCount is how many messages a bulk action on a query would reach.
func handleBulkCount(w http.ResponseWriter, r *http.Request) {
	n, err := store.QueryMessageCount(r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n, "max": store.BulkMax})
}

// handleBulk does an action to every message a query matches — "archive all
// 1,234 in Inbox › Low" — and answers with how to undo exactly that.
func handleBulk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query  string `json:"query"`
		Action string `json:"action"`
		Label  string `json:"label"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	out, err := store.BulkApply(req.Query, strings.ToLower(req.Action), req.Label)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleBulkUndo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Undo store.BulkUndo `json:"undo"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	n, err := store.BulkUndoApply(req.Undo)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": n})
}
