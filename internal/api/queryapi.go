package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/user/inboxql/internal/query"
	"github.com/user/inboxql/internal/store"
)

// registerQueryRoutes adds the query-language surface.
//
// Writes live here now that auth.Middleware refuses cross-origin state
// changes; before that gate existed, any page the user visited could have
// driven them.
func registerQueryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/query", handleQuery)
	mux.HandleFunc("/api/query/explain", handleQueryExplain)
	mux.HandleFunc("/api/query/fields", handleQueryFields)
	mux.HandleFunc("/api/query/complete", handleQueryComplete)
	mux.HandleFunc("/api/query/values", handleQueryValues)
	mux.HandleFunc("/api/queries", handleSavedQueries)
	mux.HandleFunc("/api/annotators", handleAnnotators)
	mux.HandleFunc("/api/tickets", handleTickets)
	mux.HandleFunc("/api/tickets/board", handleBoard)
}

// handleTickets is the ticket surface: list, raise, move, remove.
//
// A move is a POST of the whole ticket, so the same handler serves editing a
// title and dragging a card between columns — dragging writes the status
// field, which is the inverse of the query that defined the column.
func handleTickets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if id := r.URL.Query().Get("id"); id != "" {
			t, err := store.GetTicket(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if t == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(t)
			return
		}

		q := r.URL.Query().Get("q")
		if strings.TrimSpace(q) == "" {
			q = "-status:done -status:rejected"
		}
		limit, offset := clampPaging(r, 100, 500)
		res, err := store.RunQuery(q, limit, offset)
		if err != nil {
			writeQueryError(w, err)
			return
		}
		if res.Kind != "tickets" {
			writeQueryError(w, fmt.Errorf(
				"that query returns %s; name a ticket field such as status:", res.Kind))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res.Tickets)

	case http.MethodPost:
		var t store.Ticket
		if err := decodeJSON(w, r, &t); err != nil {
			return
		}
		if err := store.SaveTicket(&t); err != nil {
			writeQueryError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(t)

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id parameter", http.StatusBadRequest)
			return
		}
		if err := store.DeleteTicket(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBoard returns the columns and their tickets.
func handleBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, _ := clampPaging(r, 50, 200)
	columns, err := store.BoardColumns(r.URL.Query().Get("q"), limit)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(columns)
}

// handleQueryComplete answers what may be typed at a cursor position.
//
// Served by the parser rather than reimplemented in the frontend: a second
// grammar in TypeScript drifts from this one, and the failure mode is an
// editor confidently offering something the server rejects.
func handleQueryComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query().Get("q")
	// Default to the end of the text, which is where a cursor usually is.
	pos := len(q)
	if v := r.URL.Query().Get("pos"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			pos = n
		}
	}

	c, err := store.Complete(q, pos)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)
}

// handleQueryValues lists candidate values for one field.
func handleQueryValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	field := r.URL.Query().Get("field")
	if field == "" {
		http.Error(w, "missing field parameter", http.StatusBadRequest)
		return
	}

	values, err := store.FieldValues(field, r.URL.Query().Get("prefix"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if values == nil {
		values = []query.Candidate{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"field": field, "candidates": values})
}

// handleSavedQueries is the CRUD behind the workbench's saved-query rail.
func handleSavedQueries(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if name := r.URL.Query().Get("name"); name != "" {
			q, err := store.GetSavedQuery(name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if q == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(q)
			return
		}

		queries, err := store.ListSavedQueries()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(queries)

	case http.MethodPost:
		var q store.SavedQuery
		if err := decodeJSON(w, r, &q); err != nil {
			return
		}
		if err := store.SaveQuery(&q); err != nil {
			// A query that does not compile is the caller's mistake, and the
			// message says which part.
			writeQueryError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(q)

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing name parameter", http.StatusBadRequest)
			return
		}
		if err := store.DeleteSavedQuery(name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

	// The field list is the registry itself rather than a hand-kept copy of
	// the names: a client rendering help or an editor offering completions
	// needs the type and the summary, and a second list here is how the two
	// drift apart.
	type fieldInfo struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Entity  string   `json:"entity"`
		Summary string   `json:"summary"`
		Example string   `json:"example"`
		Enum    []string `json:"enum,omitempty"`
		Aliases []string `json:"aliases,omitempty"`
	}
	fields := make([]fieldInfo, 0, len(query.Registry))
	for i := range query.Registry {
		f := query.Registry[i]
		fields = append(fields, fieldInfo{
			Name: f.Name, Type: string(f.Type), Entity: f.Entity,
			Summary: f.Summary, Example: f.Example, Enum: f.Enum, Aliases: f.Aliases,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"fields":     fields,
		"folders":    store.Folders,
		"buckets":    query.Buckets,
		"groupBy":    query.GroupKeys,
		"stages":     query.StageNames,
		"sortKeys":   query.SortKeys,
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
