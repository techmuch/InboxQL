package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
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
// Off by default, and that default is the whole point. It is set once at
// startup by SetTrustLocal and only read afterwards, so no lock is needed.
var trustLocal bool

// SetTrustLocal enables or disables passwordless access for loopback clients.
//
// Call this before serving. `iql serve --trust-local` is the only thing that
// turns it on.
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

// Middleware protects routes and injects the authenticated user into the
// context. A valid session cookie always authenticates.
//
// A loopback connection may additionally skip the password, but only when
// `--trust-local` was passed. It is not enabled by default, and it cannot be,
// because a loopback peer does not mean a local user.
//
// The case that forces this: a reverse proxy on the same host — the deployment
// `iql serve` recommends in its own help text, since InboxQL terminates no TLS
// — relays every request over loopback. The peer address is then 127.0.0.1 for
// the entire internet, and auto-authenticating on it published every protected
// endpoint, mail included. Nothing about the connection distinguishes that
// from a person at the machine: the app binds loopback in both cases. Only the
// operator knows which deployment this is, so only the operator can say.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var user *store.User

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

		if user == nil && trustLocal && IsLoopback(r.RemoteAddr) && !viaProxy(r) {
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
