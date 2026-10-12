package gcal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/user/inboxql/internal/store"
)

// Signing in to Google, the desktop-app way: the browser goes to Google,
// Google sends it back to a loopback address with a code, and the code is
// exchanged here with a PKCE verifier only this process knows.
//
// The device-code flow, which would avoid the redirect, is not allowed for
// calendar scopes. A desktop client's secret is not confidential by Google's
// own description; the PKCE verifier is what an intercepted code is useless
// without.

// Flow is one sign-in in progress.
type Flow struct {
	State        string
	Verifier     string
	ClientID     string
	ClientSecret string
	Redirect     string
	Created      time.Time
}

// NewFlow starts a sign-in.
func NewFlow(clientID, clientSecret, redirect string) *Flow {
	return &Flow{State: random(24), Verifier: random(48), ClientID: strings.TrimSpace(clientID),
		ClientSecret: strings.TrimSpace(clientSecret), Redirect: redirect, Created: time.Now()}
}

func random(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// URL is where to send the browser.
func (f *Flow) URL() string {
	sum := sha256.Sum256([]byte(f.Verifier))
	q := url.Values{
		"client_id": {f.ClientID}, "redirect_uri": {f.Redirect}, "response_type": {"code"},
		"scope": {Scopes}, "state": {f.State}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		// A refresh token, and asked for every time: Google only returns one
		// on first consent otherwise, and an account reconnected without one
		// would sign out an hour later.
		"access_type": {"offline"}, "prompt": {"consent"},
	}
	return AuthURL + "?" + q.Encode()
}

// Finish exchanges the code and saves the account, returning it.
func (f *Flow) Finish(ctx context.Context, code string) (*store.CalendarAccount, error) {
	tok, err := Exchange(ctx, f.ClientID, f.ClientSecret, code, f.Verifier, f.Redirect)
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		return nil, errors.New("Google did not grant offline access; try again and allow it")
	}
	email, err := Email(ctx, tok.AccessToken)
	if err != nil || email == "" {
		return nil, errors.New("signed in, but Google did not say which account this is")
	}
	id := "google-" + strings.NewReplacer("@", "-", ".", "-").Replace(email)
	acct := store.CalendarAccount{ID: id, Provider: store.ProviderGoogle, Name: email, Email: email, Status: "ok",
		Config: map[string]string{"clientId": f.ClientID, "clientSecret": f.ClientSecret}}
	if err := store.SaveCalendarAccount(acct, tok.RefreshToken); err != nil {
		return nil, err
	}
	return &acct, nil
}

// Flows holds sign-ins in progress, each usable once and for ten minutes.
type Flows struct {
	mu sync.Mutex
	m  map[string]*Flow
}

// Add records a flow.
func (fs *Flows) Add(f *Flow) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.m == nil {
		fs.m = map[string]*Flow{}
	}
	for k, v := range fs.m {
		if time.Since(v.Created) > 10*time.Minute {
			delete(fs.m, k)
		}
	}
	fs.m[f.State] = f
}

// Take returns and forgets the flow for a state, if it is live.
func (fs *Flows) Take(state string) (*Flow, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, ok := fs.m[state]
	delete(fs.m, state)
	if !ok || time.Since(f.Created) > 10*time.Minute {
		return nil, false
	}
	return f, true
}
