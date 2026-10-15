package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/autoconfig"
	"github.com/user/inboxql/internal/gcal"
	"github.com/user/inboxql/internal/mailer"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/sync"
)

func registerAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/accounts/verify", handleAccountVerify)
	mux.HandleFunc("/api/accounts/disconnect", handleAccountDisconnect)
	mux.HandleFunc("/api/accounts/footprint", handleAccountFootprint)
	mux.HandleFunc("/api/accounts/discover", handleAccountDiscover)
	mux.HandleFunc("/api/accounts/google", handleMailGoogle)
}

// handleMailGoogle starts Sign in with Google for a Gmail account (GET says
// whether a client is already saved; POST starts). The browser comes back to
// the same callback the calendar uses, which finishes whichever was started.
func handleMailGoogle(w http.ResponseWriter, r *http.Request) {
	savedID, savedSecret := gcal.SavedClient()
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"savedClient": savedID != "", "redirect": googleRedirect()})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		AlsoCalendar bool   `json:"alsoCalendar"`
		AccountID    string `json:"accountId"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ClientID) == "" {
		body.ClientID, body.ClientSecret = savedID, savedSecret
	}
	if body.ClientID == "" {
		writeError(w, http.StatusBadRequest, "Sign in with Google needs an OAuth client of your own first; see Help → Documentation → Accounts and import")
		return
	}
	if !strings.HasSuffix(strings.TrimSpace(body.ClientID), ".apps.googleusercontent.com") {
		writeError(w, http.StatusBadRequest, "that does not look like a Google OAuth client ID (it ends in .apps.googleusercontent.com)")
		return
	}
	f := gcal.NewMailFlow(body.ClientID, body.ClientSecret, googleRedirect(), body.AlsoCalendar, body.AccountID)
	googleFlows.Add(f)
	writeJSON(w, http.StatusOK, map[string]any{"authUrl": f.URL(), "redirect": f.Redirect})
}

// keepUnsent carries over what the account form never sends — how the
// account signs in, and its sync history — so that saving an edit does not
// reset them. A new password reconnects a disconnected account.
func keepUnsent(existing, acc *account.Account) {
	if acc.Auth == "" {
		acc.Auth = existing.Auth
	}
	if acc.OAuthClientID == "" {
		acc.OAuthClientID, acc.OAuthClientSecret = existing.OAuthClientID, existing.OAuthClientSecret
	}
	if acc.LastSyncStatus == "" {
		acc.LastSyncStatus, acc.LastSyncError = existing.LastSyncStatus, existing.LastSyncError
	}
	acc.Disconnected = existing.Disconnected && acc.Password == ""
	if !acc.Disconnected && acc.LastSyncStatus == "disconnected" {
		acc.LastSyncStatus = ""
	}
	// The outgoing password, like the incoming one, is never sent to the
	// browser; blank on an edit means unchanged, unless the outgoing server
	// or its login changed.
	if acc.SMTPPassword == "" && !acc.Disconnected &&
		strings.EqualFold(existing.SMTPHost, acc.SMTPHost) && strings.EqualFold(existing.SMTPUser, acc.SMTPUser) {
		acc.SMTPPassword = existing.SMTPPassword
	}
}

// Check is one server's verdict.
type Check struct {
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Advice  string `json:"advice,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

func checkOf(err error, host string) Check {
	if err == nil {
		return Check{OK: true}
	}
	kind, advice := account.Classify(err, host)
	return Check{Kind: kind, Advice: advice, Detail: err.Error()}
}

// Verifiers, replaceable in tests.
var (
	verifyIMAP = func(acc *account.Account) error {
		c, err := sync.ConnectIMAP(acc)
		if err != nil {
			return err
		}
		c.Logout()
		return nil
	}
	verifySMTP = mailer.Verify
)

// handleAccountVerify signs in to an account's servers and reports what each
// said. It takes the form's values, saved or not; for a saved account a blank
// password means the stored one — but only while the server is unchanged, the
// same rule saving follows, so a verify cannot be used to present a stored
// credential to a host it was not given for.
func handleAccountVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var acc account.Account
	if err := decodeJSON(w, r, &acc); err != nil {
		return
	}
	if acc.ID != "" {
		existing, err := store.GetAccount(acc.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if existing != nil {
			if acc.Host == "" { // "test this saved account", as it is
				acc = *existing
			} else {
				keepUnsent(existing, &acc)
				if acc.Password == "" && sameServer(existing, &acc) {
					acc.Password = existing.Password
				}
			}
		}
	}
	acc.Disconnected = false // a verify is how a disconnected account is tried again
	if strings.TrimSpace(acc.Host) == "" {
		writeError(w, http.StatusBadRequest, "no incoming server to check")
		return
	}
	if acc.Password == "" && !acc.UsesGoogle() {
		writeError(w, http.StatusBadRequest, "no password to check with")
		return
	}

	type result struct {
		IMAP Check `json:"imap"`
		SMTP Check `json:"smtp"`
	}
	var res result
	imapDone := make(chan Check, 1)
	go func() { imapDone <- checkOf(verifyIMAP(&acc), acc.Host) }()
	if strings.TrimSpace(acc.SMTPHost) == "" {
		res.SMTP = Check{Skipped: true, Advice: "No outgoing server, so this account can receive but not send."}
	} else {
		res.SMTP = checkOf(verifySMTP(&acc), acc.SMTPHost)
	}
	res.IMAP = <-imapDone
	writeJSON(w, http.StatusOK, res)
}

func handleAccountDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if err := store.DisconnectAccount(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no account %q", id)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	acc, _ := store.GetAccount(id)
	writeJSON(w, http.StatusOK, acc.Redacted())
}

// handleAccountFootprint is what removing the account would delete, so the
// confirmation can say it.
func handleAccountFootprint(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	msgs, bytes, drafts, err := store.AccountFootprint(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "bytes": bytes, "drafts": drafts})
}

// handleAccountDiscover works out an address's servers. `ispdb=1` also asks
// Mozilla's database, which the page offers as a separate step because it
// tells Mozilla the domain.
func handleAccountDiscover(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	s, tried, err := autoconfig.Discover(ctx, email, autoconfig.Options{ISPDB: r.URL.Query().Get("ispdb") == "1"})
	if tried == nil {
		tried = []autoconfig.Attempt{}
	}
	switch {
	case errors.Is(err, autoconfig.ErrNotFound):
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "tried": tried})
	case err != nil:
		writeError(w, http.StatusBadRequest, "%v", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"found": true, "settings": s, "tried": tried})
	}
}

// accountSlug is an id from a name, the way `iql account add` makes one:
// lower case, letters and digits, runs of anything else as one dash. A name
// that is an address — the usual case now that adding starts from one —
// becomes ada-example-com rather than an id with an @ in a URL.
func accountSlug(name string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
