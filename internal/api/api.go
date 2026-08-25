// Package api holds the HTTP surface: route registration and handlers.
//
// Split out of cmd/iql so that main.go is subcommand dispatch and nothing
// else; the handlers here were previously inline alongside server startup.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/embed"
	"github.com/user/inboxql/internal/message"
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

	// Frontend Static Assets
	content, err := embed.Content()
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded frontend: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(content)))

	return mux, nil
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

func handleProfile(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(auth.UserContextKey).(*store.User)

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)

	case http.MethodPost:
		var update store.User
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
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
		if err := json.NewDecoder(r.Body).Decode(&acc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if acc.ID == "" {
			acc.ID = strings.ToLower(strings.ReplaceAll(acc.Name, " ", "-"))
		}

		// Because GET redacts the password, an edit that does not touch the
		// password field submits it empty. Treat that as "leave it alone"
		// rather than wiping a working credential; a caller that genuinely
		// wants no password can delete and recreate the account.
		if acc.Password == "" {
			if existing, err := store.GetAccount(acc.ID); err == nil && existing != nil {
				acc.Password = existing.Password
			}
		}

		if err := store.SaveAccount(&acc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
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

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		fmt.Sscanf(v, "%d", &offset)
	}

	var msgs []*message.Message
	var err error

	switch {
	case store.IsDraftFolder(folder):
		// Drafts live in their own table — they are outgoing and unsent, and
		// have never been part of the message store.
		msgs, err = store.DraftsAsMessages(accountID, limit, offset)

	default:
		// One path for both, so a folder and a dashboard cross-filter compose:
		// picking Sent while a date is selected means "sent, on that date".
		msgs, err = store.ListMessagesFiltered(accountID, store.AnalyticsFilter{
			Date:   r.URL.Query().Get("date"),
			From:   r.URL.Query().Get("from"),
			Topic:  r.URL.Query().Get("topic"),
			Folder: folder,
		}, limit, offset)
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

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	queryType := r.URL.Query().Get("type")
	filter := store.AnalyticsFilter{
		Date:  r.URL.Query().Get("date"),
		From:  r.URL.Query().Get("from"),
		Topic: r.URL.Query().Get("topic"),
	}
	var data interface{}
	var err error

	switch queryType {
	case "volume":
		data, err = store.GetTemporalVolume(filter)
	case "senders":
		data, err = store.GetTopSenders(filter)
	case "topics":
		data, err = store.GetTopicStats(filter)
	default:
		http.Error(w, "invalid analytics type", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
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
		if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if agent.ID == "" {
			agent.ID = strings.ToLower(strings.ReplaceAll(agent.Name, " ", "-"))
		}
		if err := store.SaveAgent(&agent); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
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
