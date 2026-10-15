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
	// Calendar is an iCalendar part sent beside the text — an answer to an
	// invitation — with its METHOD, which the Content-Type must repeat.
	Calendar       string
	CalendarMethod string
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

// FromDraftWithCalendar is FromDraft plus the draft's calendar part, if it
// has one.
func FromDraftWithCalendar(d *store.Draft, acc *account.Account) (*Message, error) {
	m, err := FromDraft(d, acc)
	if err != nil {
		return nil, err
	}
	if cal, err := store.GetDraftCalendar(d.ID); err == nil && cal != nil {
		m.Calendar, m.CalendarMethod = cal.ICS, cal.Method
	}
	return m, nil
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
	if m.Calendar != "" {
		// An answer to an invitation: the text, and the iCalendar REPLY as
		// an alternative body — the shape Outlook and Gmail send and read.
		const boundary = "inboxql-calendar-part"
		b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
		b.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
		writeLines(&b, m.Body)
		b.WriteString("--" + boundary + "\r\nContent-Type: text/calendar; charset=utf-8; method=" + m.CalendarMethod +
			"\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
		writeLines(&b, m.Calendar)
		b.WriteString("--" + boundary + "--\r\n")
		return []byte(b.String())
	}
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	writeLines(&b, m.Body)
	return []byte(b.String())
}

// writeLines normalises to CRLF and dot-stuffs, so a line consisting of a
// single dot cannot terminate the DATA command early.
func writeLines(b *strings.Builder, text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		b.WriteString(line)
		b.WriteString("\r\n")
	}
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

// AccessToken, when set, gives an access token for an account that signs in
// with Google. Set by the package holding the Google sign-in.
var AccessToken func(acc *account.Account) (string, error)

// connect opens an encrypted, signed-in SMTP session for the account.
//
// TLS is mandatory: implicit TLS on port 465, STARTTLS otherwise. A server
// that offers neither is refused rather than silently downgraded to plaintext,
// since the credentials and the message body would both be exposed.
func connect(acc *account.Account) (*smtp.Client, error) {
	if acc.Disconnected {
		return nil, fmt.Errorf("account %s is disconnected and cannot send; edit it to connect again", acc.ID)
	}
	host := acc.SMTPHost
	port := acc.SMTPPort
	if host == "" {
		return nil, fmt.Errorf("account %s has no SMTP host configured", acc.ID)
	}
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	tlsConfig := account.TLSConfig(host)
	dialer := &net.Dialer{Timeout: account.DialTimeout}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %w", addr, err)
	}
	// The whole conversation up to sign-in is bounded; a send then lifts it.
	conn.SetDeadline(time.Now().Add(2 * account.DialTimeout))
	if port == 465 {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("cannot connect to %s: %w", addr, err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMTP handshake failed: %w", err)
	}
	if port != 465 {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			client.Close()
			return nil, fmt.Errorf("%s does not offer STARTTLS; refusing to send credentials in plaintext", addr)
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			client.Close()
			return nil, fmt.Errorf("STARTTLS failed: %w", err)
		}
	}

	user, password := acc.SMTPLogin()
	switch {
	case acc.UsesGoogle():
		if AccessToken == nil {
			client.Close()
			return nil, errors.New("Sign in with Google is not available in this build")
		}
		tok, err := AccessToken(acc)
		if err != nil {
			client.Close()
			return nil, err
		}
		if err := client.Auth(&xoauth2Auth{user: user, token: tok}); err != nil {
			client.Close()
			return nil, fmt.Errorf("SMTP authentication failed: %w", err)
		}
	case password != "":
		if err := client.Auth(smtp.PlainAuth("", user, password, host)); err != nil {
			client.Close()
			return nil, fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	conn.SetDeadline(time.Time{})
	return client, nil
}

// Verify connects to the account's outgoing server and signs in, then hangs
// up. Nothing is sent.
func Verify(acc *account.Account) error {
	client, err := connect(acc)
	if err != nil {
		return err
	}
	client.Quit()
	return nil
}

// xoauth2Auth is XOAUTH2 for net/smtp.
type xoauth2Auth struct{ user, token string }

func (a *xoauth2Auth) Start(*smtp.ServerInfo) (string, []byte, error) {
	_, ir, _ := account.XOAuth2(a.user, a.token).Start()
	return "XOAUTH2", ir, nil
}

func (a *xoauth2Auth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		// Google's failure detail, which must be answered with an empty line.
		return []byte{}, errors.New("Google refused the access token: " + string(fromServer))
	}
	return nil, nil
}

// Send delivers the message using the account's SMTP settings.
func Send(acc *account.Account, m *Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	client, err := connect(acc)
	if err != nil {
		return err
	}
	defer client.Close()

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
