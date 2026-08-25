// Package mailer delivers outgoing messages over SMTP.
//
// This is the only code in InboxQL that causes an irreversible external effect, so
// it is deliberately small and does exactly one thing. Nothing here decides
// *whether* to send — that gate lives in the outbox approval flow — and Send
// should only ever be reached after a human has approved the draft.
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
)

// Message is a composed outgoing email.
type Message struct {
	From      string
	To        []string
	Cc        []string
	Bcc       []string
	Subject   string
	Body      string
	InReplyTo string // Message-ID of the message being replied to, if any
	Date      time.Time
}

// FromDraft builds a Message from a stored draft and its sending account.
func FromDraft(d *store.Draft, acc *account.Account) (*Message, error) {
	from := acc.Email
	if from == "" {
		from = acc.User
	}
	if from == "" {
		return nil, fmt.Errorf("account %s has no email address to send from", acc.ID)
	}
	if len(d.To) == 0 {
		return nil, errors.New("draft has no recipients")
	}

	return &Message{
		From:      from,
		To:        d.To,
		Cc:        d.Cc,
		Bcc:       d.Bcc,
		Subject:   d.Subject,
		Body:      d.Body,
		InReplyTo: d.InReplyTo,
		Date:      time.Now(),
	}, nil
}

// Recipients is every address the message is delivered to, Bcc included.
//
// Bcc recipients appear in the SMTP envelope but never in the headers — that
// is the whole point of Bcc, and getting it wrong discloses them to everyone.
func (m *Message) Recipients() []string {
	var all []string
	all = append(all, m.To...)
	all = append(all, m.Cc...)
	all = append(all, m.Bcc...)
	return all
}

// Render produces the RFC 5322 message.
func (m *Message) Render() []byte {
	var b strings.Builder

	date := m.Date
	if date.IsZero() {
		date = time.Now()
	}

	fmt.Fprintf(&b, "From: %s\r\n", m.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(m.To, ", "))
	if len(m.Cc) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(m.Cc, ", "))
	}
	// Subject is encoded so non-ASCII survives; mime.QEncoding leaves plain
	// ASCII untouched, so ordinary subjects stay readable on the wire.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", date.Format(time.RFC1123Z))
	if m.InReplyTo != "" {
		// Both headers: In-Reply-To is what clients display against,
		// References is what they thread on.
		fmt.Fprintf(&b, "In-Reply-To: %s\r\n", m.InReplyTo)
		fmt.Fprintf(&b, "References: %s\r\n", m.InReplyTo)
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")

	// Normalise to CRLF and dot-stuff, so a line consisting of a single dot in
	// the body cannot terminate the DATA command early.
	body := strings.ReplaceAll(m.Body, "\r\n", "\n")
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		b.WriteString(line)
		b.WriteString("\r\n")
	}

	return []byte(b.String())
}

// Validate checks the message is deliverable before any connection is opened.
func (m *Message) Validate() error {
	if _, err := mail.ParseAddress(m.From); err != nil {
		return fmt.Errorf("invalid From address %q: %w", m.From, err)
	}
	if len(m.Recipients()) == 0 {
		return errors.New("no recipients")
	}
	for _, addr := range m.Recipients() {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid recipient %q: %w", addr, err)
		}
	}
	if strings.TrimSpace(m.Subject) == "" {
		return errors.New("subject is empty")
	}
	return nil
}

// Send delivers the message using the account's SMTP settings.
//
// TLS is mandatory: implicit TLS on port 465, STARTTLS otherwise. A server
// that offers neither is refused rather than silently downgraded to plaintext,
// since the credentials and the message body would both be exposed.
func Send(acc *account.Account, m *Message) error {
	if err := m.Validate(); err != nil {
		return err
	}

	host := acc.SMTPHost
	port := acc.SMTPPort
	if host == "" {
		return fmt.Errorf("account %s has no SMTP host configured", acc.ID)
	}
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

	var client *smtp.Client
	var err error

	if port == 465 {
		conn, dialErr := tls.Dial("tcp", addr, tlsConfig)
		if dialErr != nil {
			return fmt.Errorf("cannot connect to %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("SMTP handshake failed: %w", err)
		}
	} else {
		client, err = smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("cannot connect to %s: %w", addr, err)
		}
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			client.Close()
			return fmt.Errorf("%s does not offer STARTTLS; refusing to send credentials in plaintext", addr)
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			client.Close()
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}
	defer client.Close()

	if acc.Password != "" {
		user := acc.User
		if user == "" {
			user = acc.Email
		}
		if err := client.Auth(smtp.PlainAuth("", user, acc.Password, host)); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	if err := client.Mail(m.From); err != nil {
		return fmt.Errorf("server rejected sender %s: %w", m.From, err)
	}
	for _, rcpt := range m.Recipients() {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("server rejected recipient %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("server rejected DATA: %w", err)
	}
	if _, err := w.Write(m.Render()); err != nil {
		w.Close()
		return fmt.Errorf("failed to write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("server rejected the message: %w", err)
	}

	return client.Quit()
}
