package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "user"

// IsLoopback reports whether the given remote address represents localhost/loopback.
func IsLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Authenticate verifies user credentials and returns a new session.
func Authenticate(username, password string) (*store.Session, error) {
	user, err := store.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid username or password")
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, errors.New("invalid username or password")
	}

	sessionID := generateSecureToken(32)
	session := &store.Session{
		ID:        sessionID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	if err := store.SaveSession(session); err != nil {
		return nil, err
	}

	return session, nil
}

// trustLocal controls whether a loopback connection may skip the password.
//
// Set once at startup by SetTrustLocal and only read afterwards, so no lock is
// needed. The decision itself lives in cli.trustDecision, which turns it on for
// a loopback listen address — reaching the port then means being on the machine
// — and off as soon as the audience widens. Whatever this is set to, a request
// that arrived through a proxy never gets it; see Middleware.
var trustLocal bool

// SetTrustLocal enables or disables passwordless access for loopback clients.
//
// Call this before serving.
func SetTrustLocal(enabled bool) { trustLocal = enabled }

// TrustLocal reports whether passwordless loopback access is enabled.
func TrustLocal() bool { return trustLocal }

// forwardedHeaders are set by every reverse proxy worth the name. Their
// presence means this connection was relayed, so the peer address describes
// the proxy rather than the client.
var forwardedHeaders = []string{
	"X-Forwarded-For",
	"X-Real-Ip",
	"Forwarded",
	"X-Forwarded-Host",
}

// viaProxy reports whether a request shows signs of having been relayed.
//
// This is defence in depth, not the primary control. A proxy that strips these
// headers would defeat it — which is exactly why passwordless access is
// opt-in rather than something we try to detect our way out of.
func viaProxy(r *http.Request) bool {
	for _, h := range forwardedHeaders {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// safeMethod reports whether a request only reads.
//
// The CORS rules stop a page from reading a cross-origin response, so a
// cross-origin GET leaks nothing even when it is authenticated. It is the
// state-changing methods that need refusing.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// crossOrigin reports whether a browser issued this request from another origin.
//
// This is the control that closes the hole passwordless access opened. Because
// the loopback path authenticates a request carrying no cookie at all,
// SameSite does nothing and any page the user visits could drive the API with
// simple requests — retarget an account's IMAP host, then trigger a sync, and
// the stored credential is presented to the attacker's server.
//
// Sec-Fetch-Site is the right signal because it is a forbidden header name:
// page JavaScript can neither set nor suppress it, so every browser-originated
// request carries it and no CLI client ever does. That asymmetry is exactly
// the line worth drawing, since passwordless access exists to serve local
// tooling rather than browsers.
//
// The Origin comparison is the fallback for browsers older than the header
// (Safari before 16.4). It compares against the request's own Host so that
// localhost:8080 and 127.0.0.1:8080 both work without configuring a list.
//
// A request with neither header is not from a browser, and gets the benefit of
// the doubt — that is `curl`, the CLI, and any local script.
func crossOrigin(r *http.Request) bool {
	switch strings.ToLower(r.Header.Get("Sec-Fetch-Site")) {
	case "cross-site", "same-site":
		// same-site counts: localhost:5173 and localhost:8080 are the same
		// site, so a Vite dev server can reach the API with a Lax cookie.
		return true
	case "same-origin", "none":
		return false
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		return !originMatchesHost(origin, r.Host)
	}
	return false
}

// originMatchesHost compares an Origin header against the host that was asked for.
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		// "null", or something unparseable. Both are opaque origins, and an
		// opaque origin is not this one.
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// Middleware protects routes and injects the authenticated user into the
// context. A valid session cookie always authenticates.
//
// A loopback connection may additionally skip the password when trustLocal is
// set, which cli.trustDecision does for a loopback listen address: reaching a
// port bound to 127.0.0.1 means being on the machine, and prompting the owner
// of the machine for a password protects nothing.
//
// The exception, which no setting overrides: a request that arrived through a
// reverse proxy. A proxy on this host — the deployment `iql start` recommends
// in its own help text, since InboxQL terminates no TLS — relays every request
// over loopback, so the peer address is 127.0.0.1 for the entire internet.
// Nothing about the connection distinguishes that from a person at the
// keyboard, which is why the forwarded headers are checked here per request
// rather than trusted away at startup.
//
// Cross-origin browser requests get neither passwordless access nor the right
// to change anything; see crossOrigin.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var user *store.User

		foreign := crossOrigin(r)

		// Refused before authenticating rather than after, so the answer does
		// not depend on whether the session cookie happened to be valid.
		if foreign && !safeMethod(r.Method) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}

		cookie, err := r.Cookie("session_id")
		if err == nil && cookie != nil && cookie.Value != "" {
			session, err := store.GetSession(cookie.Value)
			if err == nil && session != nil && !time.Now().After(session.ExpiresAt) {
				if u, err := store.GetUserByID(session.UserID); err == nil && u != nil {
					user = u
				}
			} else if session != nil {
				store.DeleteSession(session.ID)
			}
		}

		if user == nil && trustLocal && IsLoopback(r.RemoteAddr) && !viaProxy(r) && !foreign {
			if defaultUser, err := store.GetDefaultUser(); err == nil && defaultUser != nil {
				user = defaultUser
			}
		}

		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CreateInitialUser creates a default user if none exist.
func CreateInitialUser(username, password string) error {
	existing, err := store.GetUserByUsername(username)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user := &store.User{
		ID:           uuid.New().String(),
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  "Administrator",
		Email:        username,
	}

	return store.SaveUser(user)
}

func generateSecureToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return uuid.New().String()
	}
	return hex.EncodeToString(b)
}

// SetPassword sets a user's password, creating the user when absent.
//
// This is the recovery path behind `iql user passwd`: without it, a forgotten
// administrator password could only be resolved by deleting the database.
func SetPassword(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user, err := store.GetUserByUsername(username)
	if err != nil {
		return err
	}
	if user == nil {
		user = &store.User{
			ID:          uuid.New().String(),
			Username:    username,
			DisplayName: username,
			Email:       username,
		}
	}
	user.PasswordHash = string(hash)

	if err := store.SaveUser(user); err != nil {
		return err
	}

	// Existing sessions were minted against the old password, so a password
	// change that left them valid would not actually lock anyone out.
	return store.DeleteSessionsForUser(user.ID)
}
