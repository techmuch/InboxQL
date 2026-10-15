package gcal

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/mailer"
	"github.com/user/inboxql/internal/store"
	imapsync "github.com/user/inboxql/internal/sync"
)

// Gmail with Sign in with Google, rather than an app password.
//
// The account signs in to imap.gmail.com and smtp.gmail.com with XOAUTH2 and
// an access token. The refresh token is kept where a password would be —
// encrypted in the vault — and access tokens, which last an hour, are minted
// from it as needed and held only in memory.

// MailScope is full IMAP and SMTP access to Gmail. Google has no narrower
// scope that IMAP accepts.
const MailScope = "https://mail.google.com/"

func init() {
	imapsync.AccessToken = MailAccessToken
	mailer.AccessToken = MailAccessToken
}

// NewMailFlow starts a Gmail sign-in, optionally connecting the calendar in
// the same consent.
func NewMailFlow(clientID, clientSecret, redirect string, alsoCalendar bool, accountID string) *Flow {
	f := NewFlow(clientID, clientSecret, redirect)
	f.Mail, f.Calendar, f.AccountID = true, alsoCalendar, accountID
	return f
}

// Connected is what a mail sign-in set up.
type Connected struct {
	Mail     *account.Account
	Calendar *store.CalendarAccount
}

// FinishMail exchanges the code and saves the mail account — and the
// calendar account, when the flow asked for both.
func (f *Flow) FinishMail(ctx context.Context, code string) (*Connected, error) {
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

	acc, err := mailAccountFor(f.AccountID, email)
	if err != nil {
		return nil, err
	}
	acc.Auth = account.AuthGoogle
	acc.Password = tok.RefreshToken
	acc.OAuthClientID, acc.OAuthClientSecret = f.ClientID, f.ClientSecret
	acc.SMTPUser, acc.SMTPPassword = "", ""
	acc.Disconnected = false
	if acc.LastSyncStatus == "" || acc.LastSyncStatus == "disconnected" {
		acc.LastSyncStatus = "idle"
	}
	acc.LastSyncError = ""
	if err := store.SaveAccount(acc); err != nil {
		return nil, err
	}
	remember(acc.ID, tok.AccessToken, tok.ExpiresIn)
	out := &Connected{Mail: acc.Redacted()}

	if f.Calendar {
		id := "google-" + strings.NewReplacer("@", "-", ".", "-").Replace(email)
		cal := store.CalendarAccount{ID: id, Provider: store.ProviderGoogle, Name: email, Email: email, Status: "ok",
			Config: map[string]string{"clientId": f.ClientID, "clientSecret": f.ClientSecret}}
		if err := store.SaveCalendarAccount(cal, tok.RefreshToken); err != nil {
			return out, err
		}
		out.Calendar = &cal
	}
	return out, nil
}

// mailAccountFor is the account a sign-in updates: the one it was started
// for, else the one already holding this address, else a new Gmail account.
func mailAccountFor(id, email string) (*account.Account, error) {
	if id != "" {
		acc, err := store.GetAccount(id)
		if err != nil {
			return nil, err
		}
		if acc == nil {
			return nil, errors.New("the account this sign-in was for no longer exists")
		}
		if !strings.EqualFold(acc.Email, email) && acc.Email != "" {
			return nil, errors.New("you signed in to Google as " + email + ", but this account is " + acc.Email +
				"; sign in to that Google account instead")
		}
		return acc, nil
	}
	all, err := store.ListAccounts()
	if err != nil {
		return nil, err
	}
	for _, a := range all {
		if strings.EqualFold(a.Email, email) {
			return a, nil
		}
	}
	return &account.Account{
		ID: strings.NewReplacer("@", "-", ".", "-").Replace(email), Name: email, Email: email, User: email,
		Host: "imap.gmail.com", Port: 993, SSL: true, SMTPHost: "smtp.gmail.com", SMTPPort: 465,
	}, nil
}

// --- access tokens ----------------------------------------------------------

type cached struct {
	token   string
	expires time.Time
}

var (
	tokensMu sync.Mutex
	tokens   = map[string]cached{}
)

func remember(id, token string, expiresIn int) {
	tokensMu.Lock()
	defer tokensMu.Unlock()
	tokens[id] = cached{token, time.Now().Add(time.Duration(expiresIn) * time.Second)}
}

// MailAccessToken is a current access token for a Google mail account,
// refreshed when the one held has under a minute left.
func MailAccessToken(acc *account.Account) (string, error) {
	tokensMu.Lock()
	c, ok := tokens[acc.ID]
	tokensMu.Unlock()
	if ok && time.Until(c.expires) > time.Minute {
		return c.token, nil
	}
	if acc.Password == "" {
		return "", errors.New("authentication failed: this account is not signed in to Google; sign in again from Settings → Mail Accounts")
	}
	if acc.OAuthClientID == "" {
		return "", errors.New("authentication failed: this account has no Google client ID; sign in again from Settings → Mail Accounts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t, err := tokenRequest(ctx, url.Values{"client_id": {acc.OAuthClientID}, "client_secret": {acc.OAuthClientSecret},
		"refresh_token": {acc.Password}, "grant_type": {"refresh_token"}})
	if err != nil {
		return "", errors.New("authentication failed: " + err.Error())
	}
	remember(acc.ID, t.AccessToken, t.ExpiresIn)
	return t.AccessToken, nil
}

// SavedClient is an OAuth client already given to InboxQL — by a connected
// Google calendar or a Google mail account — so a second sign-in need not
// ask for it again.
func SavedClient() (id, secret string) {
	if cals, err := store.ListCalendarAccounts(); err == nil {
		for _, c := range cals {
			if c.Provider == store.ProviderGoogle && c.Config["clientId"] != "" {
				return c.Config["clientId"], c.Config["clientSecret"]
			}
		}
	}
	if accs, err := store.ListAccounts(); err == nil {
		for _, a := range accs {
			if a.UsesGoogle() && a.OAuthClientID != "" {
				return a.OAuthClientID, a.OAuthClientSecret
			}
		}
	}
	return "", ""
}
