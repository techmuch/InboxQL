package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/user/inboxql/internal/annotate"
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
	mux.HandleFunc("/api/query/terms", handleQueryTerms)
	mux.HandleFunc("/api/query/compose", handleQueryCompose)
	mux.HandleFunc("/api/query/values", handleQueryValues)
	mux.HandleFunc("/api/queries", handleSavedQueries)
	mux.HandleFunc("/api/annotators", handleAnnotators)
	mux.HandleFunc("POST /api/annotators/run", handleAnnotatorRun)
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

// handleQueryTerms splits a query into its top-level terms.
//
// Backs the filter pills. They render from the query itself rather than from a
// parallel list, so what is shown and what runs cannot disagree — which is
// exactly how a top-sender click came to change the results without appearing
// in the query bar.
func handleQueryTerms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("q")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(composed(q))
}

// composed is the one response shape every composition endpoint returns: the
// query, its terms and its stages. A caller that changed one of them almost
// always wants to re-render both.
func composed(q string) map[string]any {
	return map[string]any{"query": q, "terms": query.Terms(q), "stages": query.Stages(q)}
}

// handleQueryCompose adds, removes or replaces a term in a query.
//
// Composition lives here rather than in the frontend because three separate
// TypeScript implementations of it have existed in this project and all three
// were wrong in the same way — they split on whitespace, so a quoted value
// containing a space stopped being one term. The lexer already knows where a
// term ends.
//
// A click is about to re-run the query anyway, so the round trip is free.
func handleQueryCompose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	params := r.URL.Query()
	q := params.Get("q")

	// `value` is a pill's raw input; the term it becomes is assembled here so
	// the quoting rules live with the grammar. Anything else is a term already.
	term := params.Get("term")
	if params.Has("value") {
		term = query.FormatTerm(params.Get("field"), params.Get("value"),
			params.Get("negated") == "true")
	}

	switch {
	case params.Has("at"):
		// A pill edits the term at its own offset. Matching by text would pick
		// the wrong one when a query carries two terms that read alike.
		at, err := strconv.Atoi(params.Get("at"))
		if err != nil {
			http.Error(w, "at must be an offset", http.StatusBadRequest)
			return
		}
		q = query.ReplaceAt(q, at, term)
	case params.Get("add") != "":
		q = query.WithTerm(q, params.Get("add"))
	case params.Get("remove") != "":
		q = query.WithoutTerm(q, params.Get("remove"))
	case params.Get("stage") != "":
		// A pipeline stage, not a filter term. Adding a terminal one replaces
		// whichever terminal is already there, so a toggle can never build the
		// two-aggregate query the planner rejects.
		q = query.WithStage(q, params.Get("stage"))
	case params.Get("dropStage") != "":
		q = query.WithoutStage(q, params.Get("dropStage"))
	case params.Has("field"):
		// An empty `term` with a field removes that field's term, which is how
		// "show me everything" clears a folder.
		q = query.ReplaceField(q, params.Get("field"), term)
	default:
		http.Error(w, "give one of at, add, remove, field, stage or dropStage",
			http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(composed(q))
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

// handleAnnotators is the annotator surface: list, define, remove.
//
// Read-only until now, which made the product's actual AI feature — the thing
// that turns an unstructured body into queryable structured data — reachable
// only from the CLI, while a graph editor that cannot execute anything had a
// tab of its own.
func handleAnnotators(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listAnnotators(w, r)
	case http.MethodPost:
		saveAnnotator(w, r)
	case http.MethodDelete:
		deleteAnnotator(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func listAnnotators(w http.ResponseWriter, r *http.Request) {
	annotators, err := store.ListAnnotators()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type row struct {
		*store.Annotator
		Progress *store.AnnotatorProgress `json:"progress"`
		// Scope and ConsentMissing describe what running this would do:
		// whether it sends message bodies off the machine, and whether it is
		// currently allowed to. Answered per annotator, from its own profile.
		Scope          string `json:"scope,omitempty"`
		Endpoint       string `json:"endpoint,omitempty"`
		ConsentMissing bool   `json:"consentMissing,omitempty"`
		// ProfileMissing flags an annotator naming a profile that no longer
		// exists — a run would fail, and the list is where that should show.
		ProfileMissing bool `json:"profileMissing,omitempty"`
	}
	out := make([]row, 0, len(annotators))
	for _, a := range annotators {
		p, err := store.Progress(a, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		item := row{Annotator: a, Progress: p}
		if a.Engine == store.EngineLLM {
			cfg, err := store.GetLLMConfigFor(a.Profile)
			if err != nil {
				item.ProfileMissing = true
			} else {
				item.Scope = cfg.Scope()
				if cfg.IsRemote() {
					item.Endpoint = cfg.Endpoint
					item.ConsentMissing = !a.AllowRemote
				}
			}
		}
		out = append(out, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// saveAnnotator defines or edits one.
//
// Editing the instructions bumps the version, which is what marks earlier
// results stale — the store decides that, not this handler, so the CLI and the
// UI cannot disagree about when a re-run is needed.
func saveAnnotator(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		Kind         string `json:"kind"`
		Engine       string `json:"engine"`
		Instructions string `json:"instructions"`
		SchemaJSON   string `json:"schemaJson"`
		Profile      string `json:"profile"`
		Model        string `json:"model"`
		AllowRemote  bool   `json:"allowRemote"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Instructions) == "" {
		writeError(w, http.StatusBadRequest,
			"instructions are required: a rule is a query expression, an LLM annotator is a prompt")
		return
	}
	// Rejected here so a typo is a 400 rather than an annotator that exists
	// and fails at run time.
	if req.Profile != "" {
		if _, err := store.ResolveLLMProfile(req.Profile); err != nil {
			writeError(w, http.StatusBadRequest, "%v", err)
			return
		}
	}
	// A rule's instruction is a query, so it is validated as one. An invalid
	// rule saved now is an annotator that matches nothing later, which reads
	// as "found no mail" rather than as a mistake.
	if req.Engine == store.EngineRule {
		if err := store.ValidateQuery(req.Instructions); err != nil {
			writeQueryError(w, err)
			return
		}
	}

	a := &store.Annotator{
		Name: req.Name, Kind: req.Kind, Engine: req.Engine,
		Instructions: req.Instructions, SchemaJSON: req.SchemaJSON,
		Profile: req.Profile, Model: req.Model, AllowRemote: req.AllowRemote,
	}
	if a.Kind == "" {
		a.Kind = store.KindLabel
	}
	if a.Engine == "" {
		a.Engine = store.EngineRule
	}
	if err := store.SaveAnnotator(a); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

func deleteAnnotator(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := store.DeleteAnnotator(name); err != nil {
		writeError(w, http.StatusNotFound, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"removed": name})
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

// handleAnnotatorRun applies an annotator, or reports what applying it would do.
//
// # Why this is synchronous, and bounded
//
// A rule is one SQL statement, so running it is instant. An LLM annotator is a
// request per message, which over a large mailbox is minutes — so this takes a
// limit and the caller works through a backlog in batches, watching progress
// between them.
//
// The alternative is a background job system with its own state, retries and
// status endpoint. That is a real thing to build, and pretending to have it by
// firing a goroutine and returning 202 would give the UI a progress bar with
// nothing behind it. A bounded synchronous run is smaller and honest.
func handleAnnotatorRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Scope  string `json:"scope"`
		Limit  int    `json:"limit"`
		DryRun bool   `json:"dryRun"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.DryRun {
		plan, err := annotate.Describe(req.Name, req.Scope)
		if err != nil {
			writeError(w, http.StatusBadRequest, "%v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(plan)
		return
	}

	// Bounded by default. An unbounded LLM run started from a click is a
	// request that never returns and a bill nobody agreed to.
	if req.Limit <= 0 {
		req.Limit = 200
	}

	out, err := annotate.Run(r.Context(), req.Name, annotate.Options{
		Scope: req.Scope, Limit: req.Limit,
	})
	if err != nil {
		// A refused remote run is the caller's decision to make, not a server
		// fault, so it comes back as a 400 with the sentence that explains it.
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
