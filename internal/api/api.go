// Package api holds the HTTP surface: route registration and handlers.
//
// Split out of cmd/iql so that main.go is subcommand dispatch and nothing
// else; the handlers here were previously inline alongside server startup.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/embed"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/query"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/sync"
)

// syncManager bounds concurrent IMAP connections per host for syncs triggered
// over HTTP.
var syncManager = sync.NewSyncManager(5)

// Router builds the full HTTP handler: public routes, authenticated API routes,
// and the embedded frontend.
func Router() (http.Handler, error) {
	mux := http.NewServeMux()

	// Public API Routes
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/logout", handleLogout)
	mux.HandleFunc("/api/version", handleVersion)

	// Sub-mux for the /api/accounts/* subtree only. Every other protected route
	// is an exact-match registration on the parent mux below, which always wins
	// over a subtree pattern, so listing them here too would be dead weight.
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/accounts/stats", handleAccountStats)
	apiMux.HandleFunc("/api/accounts/sync", handleAccountSync)

	// Register the protected mux with auth middleware
	mux.Handle("/api/accounts", auth.Middleware(http.HandlerFunc(handleAccounts)))
	mux.Handle("/api/accounts/", auth.Middleware(apiMux))
	mux.Handle("/api/messages", auth.Middleware(http.HandlerFunc(handleMessages)))
	mux.Handle("/api/messages/counts", auth.Middleware(http.HandlerFunc(handleFolderCounts)))
	mux.Handle("/api/messages/flags", auth.Middleware(http.HandlerFunc(handleMessageFlags)))
	mux.Handle("/api/contacts", auth.Middleware(http.HandlerFunc(handleContacts)))
	mux.Handle("/api/message", auth.Middleware(http.HandlerFunc(handleMessage)))
	mux.Handle("/api/message/attachments", auth.Middleware(http.HandlerFunc(handleMessageAttachments)))
	mux.Handle("/api/profile", auth.Middleware(http.HandlerFunc(handleProfile)))
	mux.Handle("/api/analytics", auth.Middleware(http.HandlerFunc(handleAnalytics)))
	mux.Handle("/api/settings", auth.Middleware(http.HandlerFunc(handleSettings)))
	mux.Handle("/api/agents", auth.Middleware(http.HandlerFunc(handleAgents)))
	mux.Handle("/api/data", auth.Middleware(http.HandlerFunc(handleData)))

	// Import routes are authenticated as a group: they read the user's mail
	// client and start jobs that write to the database.
	importMux := http.NewServeMux()
	registerImportRoutes(importMux)
	mux.Handle("/api/import/", auth.Middleware(importMux))

	errorMux := http.NewServeMux()
	registerErrorRoutes(errorMux)
	mux.Handle("/api/errors", auth.Middleware(errorMux))

	llmMux := http.NewServeMux()
	registerLLMRoutes(llmMux)
	mux.Handle("/api/llm/", auth.Middleware(llmMux))

	// The query language surface; see registerQueryRoutes.
	//
	// Mounted route by route rather than under a "/api/" prefix so that adding
	// a handler to the inner mux is not enough to expose it — a new path has
	// to be listed here too. That is deliberate for a surface that now writes
	// as well as reads.
	queryMux := http.NewServeMux()
	registerQueryRoutes(queryMux)
	for _, route := range []string{
		"/api/query", "/api/query/explain", "/api/query/fields",
		"/api/query/complete", "/api/query/values",
		"/api/query/terms", "/api/query/compose",
		"/api/queries", "/api/annotators", "/api/annotators/run",
		"/api/tickets", "/api/tickets/board",
	} {
		mux.Handle(route, auth.Middleware(queryMux))
	}

	// Frontend Static Assets
	content, err := embed.Content()
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded frontend: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(content)))

	return mux, nil
}

// decodeJSON reads a JSON body, insisting that it was sent as JSON.
//
// A third barrier behind the origin checks in auth.Middleware, and an
// independent one: a cross-origin request may only set Content-Type to
// text/plain, form-urlencoded or multipart without triggering a preflight, and
// the preflight fails because no CORS headers are served. Handlers that decode
// whatever arrives regardless of Content-Type give that restriction away for
// nothing.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if mediaType, _, err := mime.ParseMediaType(ct); err != nil || mediaType != "application/json" {
			err := fmt.Errorf("expected Content-Type: application/json")
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
			return err
		}
	}
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return err
	}
	return nil
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		log.Printf("Login error: failed to decode JSON: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	session, err := auth.Authenticate(creds.Username, creds.Password)
	if err != nil {
		log.Printf("Login error: authentication failed for %s: %v", creds.Username, err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	user, err := store.GetUserByID(session.UserID)
	if err != nil {
		log.Printf("Login error: failed to get user by ID: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil {
		store.DeleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	w.WriteHeader(http.StatusNoContent)
}

// VersionInfo carries the running version, revision, and dev-mode flag for clients.
type VersionInfo struct {
	Version    string `json:"version"`
	Revision   string `json:"revision,omitempty"`
	Dev        bool   `json:"dev"`
	InstanceID string `json:"instanceId"`
}

var currentVersionInfo = VersionInfo{
	Version: "0.0.50",
}

// SetVersionInfo sets the version metadata served at /api/version.
func SetVersionInfo(info VersionInfo) {
	currentVersionInfo = info
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	json.NewEncoder(w).Encode(currentVersionInfo)
}

func handleProfile(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(auth.UserContextKey).(*store.User)

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)

	case http.MethodPost:
		var update store.User
		if err := decodeJSON(w, r, &update); err != nil {
			return
		}
		user.DisplayName = update.DisplayName
		user.Email = update.Email
		user.ProfileImageURL = update.ProfileImageURL
		if err := store.SaveUser(user); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		accounts, err := store.ListAccounts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Passwords never leave the server. Previously the full account
		// struct, plaintext password included, was serialised to any
		// authenticated client.
		json.NewEncoder(w).Encode(account.RedactAll(accounts))

	case http.MethodPost:
		var acc account.Account
		if err := decodeJSON(w, r, &acc); err != nil {
			return
		}

		// An omitted id used to be derived from the name, so a create whose
		// slug collided with an existing account silently overwrote it. An
		// update now has to name the account it is updating.
		derived := false
		if acc.ID == "" {
			acc.ID = strings.ToLower(strings.ReplaceAll(acc.Name, " ", "-"))
			derived = true
		}

		existing, err := store.GetAccount(acc.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if existing != nil && derived {
			http.Error(w, "an account with this name already exists; pass its id to update it",
				http.StatusConflict)
			return
		}

		if existing != nil {
			// Because GET redacts the password, an edit that does not touch
			// the password field submits it empty. Treat that as "leave it
			// alone" rather than wiping a working credential.
			//
			// Except when the server changed. A password for imap.gmail.com is
			// not a password for some other host, and carrying it across would
			// present the user's credential to whatever was named here. That
			// is wrong on its own terms, and it independently breaks the
			// retarget-then-sync path that made the CSRF hole worth exploiting.
			if acc.Password == "" {
				if sameServer(existing, &acc) {
					acc.Password = existing.Password
				} else {
					http.Error(w, "the IMAP host or port changed, so the stored password no longer applies; send the password again",
						http.StatusBadRequest)
					return
				}
			}
		}

		if err := store.SaveAccount(&acc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Content-Type before WriteHeader: headers set after the status line
		// has been written are discarded, and this response was going out as
		// text/plain with a JSON body.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(acc.Redacted())

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id parameter", http.StatusBadRequest)
			return
		}
		if err := store.DeleteAccount(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// sameServer reports whether an update leaves the account pointed at the same
// mailbox, and so may keep the credential stored for it.
func sameServer(existing, updated *account.Account) bool {
	return strings.EqualFold(existing.Host, updated.Host) &&
		existing.Port == updated.Port &&
		strings.EqualFold(existing.User, updated.User)
}

func handleAccountStats(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	stats, err := store.GetAccountStats(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleAccountSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	acc, err := store.GetAccount(id)
	if err != nil || acc == nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	go syncManager.StartSync(acc)

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, "Sync started")
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	folder := r.URL.Query().Get("folder")

	if !store.ValidFolder(folder) {
		http.Error(w, "unknown folder", http.StatusBadRequest)
		return
	}

	limit, offset := clampPaging(r, 50, 500)

	var msgs []*message.Message
	var err error

	// A `q` parameter hands the whole selection to the query language, which
	// is how the dashboard's cross-filters arrive now: the UI composes a query
	// string the user can see and edit rather than a hidden filter struct.
	if q := r.URL.Query().Get("q"); q != "" && !store.IsDraftFolder(folder) {
		res, err := store.RunQuery(q, limit, offset)
		if err != nil {
			writeQueryError(w, err)
			return
		}
		if res.Kind != "messages" {
			// An aggregate reached the message endpoint. Say so rather than
			// returning an empty list that looks like "no matches".
			writeQueryError(w, fmt.Errorf(
				"this query aggregates; call /api/query for %s results", res.Kind))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res.Messages)
		return
	}

	switch {
	case store.IsDraftFolder(folder):
		// Drafts live in their own table — they are outgoing and unsent, and
		// have never been part of the message store.
		msgs, err = store.DraftsAsMessages(accountID, limit, offset)

	default:
		// The legacy parameters are composed into a query rather than served by
		// their own SQL. There is one filter implementation now, so a folder
		// and a cross-filter compose because they are terms in one expression
		// rather than fields in a struct three functions each read differently.
		res, qerr := store.RunQuery(legacyFilterQuery(r, accountID, folder), limit, offset)
		if qerr != nil {
			writeQueryError(w, qerr)
			return
		}
		msgs = res.Messages
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []*message.Message{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

// handleFolderCounts backs the mailbox sidebar.
//
// All folders in one response: six round trips to render one list would be
// silly, and the counts have to agree with each other at a single moment or
// the sidebar contradicts itself.
func handleFolderCounts(w http.ResponseWriter, r *http.Request) {
	counts, err := store.FolderCounts(r.URL.Query().Get("accountId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(counts)
}

// handleMessage serves a single message by id, backing the Thread Focus view.
//
// This was previously an empty function body: the route was registered and
// returned 200 with no content, so every caller silently received nothing.
func handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}

	msg, err := store.GetMessageByID(id)
	if err != nil {
		log.Printf("Error loading message %s: %v", id, err)
		http.Error(w, "failed to load message", http.StatusInternalServerError)
		return
	}
	if msg == nil {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

// legacyFilterQuery composes the pre-language query parameters into an
// expression.
//
// `q` wins when it is given; these remain for callers written before the
// language existed. Values are quoted rather than interpolated bare, because a
// sender address or a subject word can contain characters the parser would
// otherwise read as syntax.
func legacyFilterQuery(r *http.Request, accountID, folder string) string {
	if q := r.URL.Query().Get("q"); q != "" {
		return q
	}

	var terms []string
	add := func(field, value string) {
		if value != "" {
			terms = append(terms, field+":\""+strings.ReplaceAll(value, `"`, `""`)+"\"")
		}
	}
	add("account", accountID)
	add("on", r.URL.Query().Get("date"))
	add("from", r.URL.Query().Get("from"))
	add("subject", r.URL.Query().Get("topic"))
	if folder != "" && folder != store.FolderAll {
		add("folder", folder)
	}
	return strings.Join(terms, " ")
}

// analyticsQueries are the dashboard's widgets, as queries.
//
// Each was a hand-written function with its own copy of the filter logic, and
// the three disagreed with each other about what `from` meant. They are three
// strings now, and adding a widget is a fourth rather than a fourth function.
var analyticsQueries = map[string]string{
	"volume": "| count by day",
	// The user's own addresses are excluded because a chart of who writes to
	// you should not be topped by you. me() is that exclusion, expressed once.
	"senders": "-from:me() | top from 10",
	"topics":  "| top topic 50",
}

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	stage, ok := analyticsQueries[r.URL.Query().Get("type")]
	if !ok {
		http.Error(w, "invalid analytics type", http.StatusBadRequest)
		return
	}

	// Only the filter half. Desk and the dashboard share one query, and that
	// query may carry a pipeline — `| timeline`, `| count by week`, `| top
	// domain 5`. Concatenating this widget's own aggregate onto one of those
	// produced two terminal stages, which the planner rejects, so every widget
	// returned a 400 the moment anything in Desk was aggregated.
	//
	// The filter is the part that means "which mail"; the stage is this
	// widget's own question about it.
	filter := query.FilterOf(legacyFilterQuery(r, "", ""))

	// Analytics charts mail. One shared query means a ticket or draft query can
	// arrive here, and silently charting something else would be worse than
	// saying so.
	if parsed, err := query.Parse(filter); err == nil && parsed.Entity() != query.EntityMessage {
		writeQueryError(w, fmt.Errorf(
			"this query is about %ss; analytics charts mail", parsed.Entity()))
		return
	}

	expr := strings.TrimSpace(filter + " " + stage)

	res, err := store.RunQuery(expr, 0, 0)
	if err != nil {
		// me() has nothing to resolve to until an account has an address, and
		// a dashboard that errors on a fresh install is worse than one that
		// shows everyone.
		if strings.Contains(err.Error(), "me()") {
			res, err = store.RunQuery(strings.TrimSpace(filter+" | top from 10"), 0, 0)
		}
		if err != nil {
			writeQueryError(w, err)
			return
		}
	}

	groups := res.Groups
	if r.URL.Query().Get("type") == "topics" {
		groups = withoutIgnoredWords(groups)
	}

	// The dashboard reads {label, value}, which is what a group already is.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

// withoutIgnoredWords drops the topic words the user asked not to see.
//
// Applied to the result rather than compiled into the query: it is a display
// preference, and pushing it into the language would mean every topic query
// silently honoured a setting the query does not mention.
func withoutIgnoredWords(groups []store.QueryGroup) []store.QueryGroup {
	setting, err := store.GetSetting("ignore_words")
	if err != nil || strings.TrimSpace(setting) == "" {
		return groups
	}
	ignored := map[string]bool{}
	for _, w := range strings.Split(strings.ToLower(setting), ",") {
		if w = strings.TrimSpace(w); w != "" {
			ignored[w] = true
		}
	}

	out := make([]store.QueryGroup, 0, len(groups))
	for _, g := range groups {
		if g.Label != "" && !ignored[strings.ToLower(g.Label)] {
			out = append(out, g)
		}
	}
	return out
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		val, err := store.GetSetting(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"value": val})

	case http.MethodPost:
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			return
		}
		if err := store.UpdateSetting(req.Key, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			agent, err := store.GetAgent(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if agent == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(agent)
			return
		}

		agents, err := store.ListAgents()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agents)

	case http.MethodPost:
		var agent store.Agent
		if err := decodeJSON(w, r, &agent); err != nil {
			return
		}
		if agent.ID == "" {
			agent.ID = strings.ToLower(strings.ReplaceAll(agent.Name, " ", "-"))
		}
		if err := store.SaveAgent(&agent); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(agent)

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id parameter", http.StatusBadRequest)
			return
		}
		if err := store.DeleteAgent(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMessageAttachments lists what a message carried.
//
// A row with no storagePath is a record that the message had an attachment InboxQL
// chose not to keep — too large, or attachments disabled for that import —
// rather than a broken reference. The viewer renders the distinction.
func handleMessageAttachments(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}

	list, err := store.ListAttachments(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*store.Attachment{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func handleData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := store.EraseSyncedData(); err != nil {
		log.Printf("Failed to erase synced data: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMessageFlags marks a set of messages read, unread or starred.
//
// The first bulk action in InboxQL. Selection existed before it did — a count
// and a Clear button with nothing behind them — which made the checkbox column
// decoration.
//
// Local only. There is no IMAP write-back, so the response says how many rows
// changed and the caller is expected to say so rather than implying the server
// was told.
func handleMessageFlags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		IDs  []string `json:"ids"`
		Flag string   `json:"flag"`
		On   bool     `json:"on"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "no messages given")
		return
	}
	// Bounded so one request cannot be asked to rewrite the whole mailbox in a
	// single transaction.
	if len(req.IDs) > 1000 {
		writeError(w, http.StatusBadRequest,
			"too many messages in one request (%d); 1000 at a time", len(req.IDs))
		return
	}

	changed, err := store.SetMessageFlag(req.IDs, req.Flag, req.On)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"changed": changed})
}

// handleContacts records a correction to a contact.
//
// Write-only on purpose. Reading contacts goes through /api/query like every
// other entity — a second read path would be a second set of filtering,
// ordering and paging semantics to keep in step with the first. But the query
// language reads and does not write, so a correction needs somewhere to go.
//
// Everything written here is attributed to a person, which is what stops the
// next rule or model run from undoing it.
func handleContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Address     string `json:"address"`
		DisplayName string `json:"displayName"`
		FirstName   string `json:"firstName"`
		LastName    string `json:"lastName"`
		Phone       string `json:"phone"`
		Org         string `json:"org"`
		Title       string `json:"title"`
		Kind        string `json:"kind"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Address) == "" {
		writeError(w, http.StatusBadRequest, "address is required")
		return
	}
	switch req.Kind {
	case "", store.KindPerson, store.KindOrganization, store.KindSystem, store.KindUnknown:
	default:
		writeError(w, http.StatusBadRequest,
			"kind must be person, organization, system or unknown")
		return
	}

	c := &store.Contact{
		Address: req.Address, DisplayName: req.DisplayName,
		FirstName: req.FirstName, LastName: req.LastName, Phone: req.Phone,
		Org: req.Org, Title: req.Title, Kind: req.Kind,
	}
	if err := store.SaveContact(c, store.ContactFromHuman); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}

	saved, err := store.GetContact(req.Address)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saved)
}
