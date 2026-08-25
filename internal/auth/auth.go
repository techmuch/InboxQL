package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "user"

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

// Middleware protects routes and injects the authenticated user into the context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		session, err := store.GetSession(cookie.Value)
		if err != nil || session == nil || time.Now().After(session.ExpiresAt) {
			if session != nil {
				store.DeleteSession(session.ID)
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := store.GetUserByID(session.UserID)
		if err != nil || user == nil {
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
