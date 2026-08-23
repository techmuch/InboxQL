package account

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

	SSL            bool   `json:"ssl"`
	SMTPHost       string `json:"smtpHost"`
	SMTPPort       int    `json:"smtpPort"`
	LastSyncStatus string `json:"lastSyncStatus"`
	LastSyncError  string `json:"lastSyncError"`
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
