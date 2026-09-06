package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/user/inboxql/internal/query"
	"github.com/user/inboxql/internal/store"
)

// registerQueryRoutes adds the query-language surface.
//
// Read-only, deliberately. Defining and running annotators is a write, and the
// HTTP API currently has no CSRF defence — the passwordless-loopback default
// authenticates a cross-origin request that carries no cookie at all. Until
// that is closed, the annotator write surface stays on the CLI, where the
// caller is already on the machine.
func registerQueryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/query", handleQuery)
	mux.HandleFunc("/api/query/explain", handleQueryExplain)
	mux.HandleFunc("/api/query/fields", handleQueryFields)
	mux.HandleFunc("/api/annotators", handleAnnotators)
}

// clampPaging reads limit and offset, refusing the values that would ask the
// database for the whole mailbox at once.
//
// Previously these were read with fmt.Sscanf and its error ignored, so
// `?limit=99999999` was honoured and `?offset=-5` reached SQL unaltered.
func clampPaging(r *http.Request, defaultLimit, maxLimit int) (limit, offset int) {
	limit, offset = defaultLimit, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// handleQuery runs a query expression.
//
// A bad query is a 400 with the parser's message and the offset it failed at,
// not a 500: the caller wrote it, and the caret position is the useful part.
func handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit, offset := clampPaging(r, 50, 500)
	res, err := store.RunQuery(r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		writeQueryError(w, err)
		return
	}

	// The compiled SQL is for `explain`, not for every response.
	res.SQL, res.Args = "", nil

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func handleQueryExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := store.ExplainQuery(r.URL.Query().Get("q"))
	if err != nil {
		writeQueryError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// handleQueryFields backs autocomplete in the search bar.
func handleQueryFields(w http.ResponseWriter, r *http.Request) {
	annotators, err := store.ListAnnotators()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	labels := []string{}
	for _, a := range annotators {
		labels = append(labels, a.Name)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"fields":     query.KnownFields,
		"folders":    store.Folders,
		"buckets":    []string{"hour", "day", "week", "month", "year"},
		"groupBy":    []string{"from", "domain", "to", "cc", "account", "mailbox", "label", "thread", "subject"},
		"stages":     []string{"count", "top", "sort", "limit", "thread", "participants", "extract", "series", "sum", "avg", "min", "max"},
		"annotators": labels,
		"fullText":   store.FullTextAvailable(),
	})
}

// handleAnnotators lists annotators and their coverage, read-only.
func handleAnnotators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	annotators, err := store.ListAnnotators()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type row struct {
		*store.Annotator
		Progress *store.AnnotatorProgress `json:"progress"`
	}
	out := make([]row, 0, len(annotators))
	for _, a := range annotators {
		p, err := store.Progress(a, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, row{Annotator: a, Progress: p})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// writeQueryError reports a malformed query as a 400 carrying the position.
func writeQueryError(w http.ResponseWriter, err error) {
	payload := map[string]any{"error": err.Error()}
	if perr, ok := err.(*query.Error); ok {
		payload["position"] = perr.Pos
		payload["message"] = perr.Msg
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(payload)
}
