package account

import "time"

// Account represents an email account configured for synchronization.
type Account struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
	User  string `json:"user"`

	// Password is plaintext in memory and encrypted at rest by internal/vault;
	// the store encrypts on save and decrypts on load, so callers here always
	// see plaintext.
	//
	// It stays JSON-decodable because the account form submits it, but it must
	// never be written back out to a client. Use Redacted for that — handlers
	// returning accounts over HTTP are responsible for calling it.
	Password string `json:"password,omitempty"`

	SSL      bool   `json:"ssl"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`

	// SMTPUser and SMTPPassword are the outgoing server's own login, for the
	// providers where it differs from the incoming one. Blank means "the same
	// as IMAP", which is nearly everyone. The password is encrypted at rest
	// and redacted like Password.
	SMTPUser     string `json:"smtpUser,omitempty"`
	SMTPPassword string `json:"smtpPassword,omitempty"`

	// Auth is how the account signs in: "password" (the default, also when
	// blank) or "google", where Password holds a refresh token from Sign in
	// with Google and the servers are given an access token over XOAUTH2.
	Auth string `json:"auth,omitempty"`
	// OAuthClientID and OAuthClientSecret are the Google client the refresh
	// token was issued to; a refresh needs both. A desktop client's secret is
	// not confidential by Google's own description.
	OAuthClientID     string `json:"oauthClientId,omitempty"`
	OAuthClientSecret string `json:"-"`

	// Disconnected accounts keep their mail and have no login: they are not
	// synced and cannot send. Set by Disconnect, which also clears the
	// passwords; cleared by saving a password again.
	Disconnected bool `json:"disconnected,omitempty"`

	LastSyncStatus string `json:"lastSyncStatus"`
	LastSyncError  string `json:"lastSyncError"`
	// LastSyncedAt is when a sync last finished without error. Not the date
	// of the newest message, which is what the card used to show under this
	// name.
	LastSyncedAt *time.Time `json:"lastSyncedAt,omitempty"`
}

// UsesGoogle reports whether the account signs in with Google rather than a
// password.
func (a *Account) UsesGoogle() bool { return a.Auth == AuthGoogle }

// Sign-in methods.
const (
	AuthPassword = "password"
	AuthGoogle   = "google"
)

// SMTPLogin is the username and password for the outgoing server: its own
// when set, the incoming server's otherwise.
func (a *Account) SMTPLogin() (user, password string) {
	user, password = a.SMTPUser, a.SMTPPassword
	if user == "" {
		user = a.User
	}
	if user == "" {
		user = a.Email
	}
	if password == "" {
		password = a.Password
	}
	return user, password
}

// Redacted returns a copy of the account with the password cleared, safe to
// serialise to an API client.
//
// Combined with omitempty the field disappears from the payload entirely, so
// the UI can tell "not sent" from "deliberately blank" and leave an untouched
// password field alone on save.
func (a *Account) Redacted() *Account {
	if a == nil {
		return nil
	}
	clone := *a
	clone.Password = ""
	clone.SMTPPassword = ""
	return &clone
}

// RedactAll returns copies of the given accounts with passwords cleared.
func RedactAll(accounts []*Account) []*Account {
	redacted := make([]*Account, 0, len(accounts))
	for _, a := range accounts {
		redacted = append(redacted, a.Redacted())
	}
	return redacted
}

// AccountStats provides statistics for an account.
type AccountStats struct {
	TotalMessages  int    `json:"totalMessages"`
	UnreadMessages int    `json:"unreadMessages"`
	StorageSize    int64  `json:"storageSize"`
	LastSync       string `json:"lastSync"`
}
