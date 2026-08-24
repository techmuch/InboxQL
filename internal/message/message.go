package message

import "time"

// Message represents a single email message.
type Message struct {
	ID             string `json:"id"`             // Unique ID for the message in our system
	AccountID      string `json:"accountId"`      // ID of the account this message belongs to
	UID            uint32 `json:"uid"`            // IMAP UID
	MessageID      string `json:"messageId"`      // RFC822 Message-ID header
	ContentHash    string `json:"contentHash"`    // SHA-256 hash of the normalized message body
	NormalizedBody string `json:"normalizedBody"` // Normalized message body used for hashing

	From     string    `json:"from"`
	To       []string  `json:"to"`
	Cc       []string  `json:"cc"`
	Bcc      []string  `json:"bcc"`
	Subject  string    `json:"subject"`
	Date     time.Time `json:"date"`
	Body     string    `json:"body"` // Plain text body for indexing/display
	HTMLBody string    `json:"htmlBody"`
	Header   []byte    `json:"header"` // Raw message headers
	Flags    []string  `json:"flags"`
	Size     uint32    `json:"size"`
	// Mailbox is the folder this message was read from, e.g. "INBOX" or
	// "Sent Messages". Empty for rows written before folders were tracked.
	Mailbox      string    `json:"mailbox,omitempty"`
	InternalDate time.Time `json:"internalDate"`
}
