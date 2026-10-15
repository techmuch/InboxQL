package account

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"time"
)

// DialTimeout bounds how long connecting to a mail server may take. Without
// one a server that accepts the connection and then says nothing holds a
// verify, or a sync, open indefinitely.
var DialTimeout = 20 * time.Second

// TestRoots, when set, replaces the system's trusted certificates. Tests use
// it to trust a server they started; nothing else should.
var TestRoots *x509.CertPool

// TLSConfig is the configuration every mail connection uses: the certificate
// verified against the host the person typed, TLS 1.2 or later.
func TLSConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: TestRoots}
}

// Problem kinds, as a verify reports them. The kind is for code; Advice is
// for the person.
const (
	ProblemAuth        = "auth"
	ProblemHost        = "host"
	ProblemUnreachable = "unreachable"
	ProblemTLS         = "tls"
	ProblemPlaintext   = "plaintext"
	ProblemOther       = "other"
)

// Classify sorts a connection error into a kind and a sentence saying what
// to do about it.
//
// The distinction that matters most is a rejected password against
// everything else: a server that refused the credential will refuse it again,
// and the fix is the person's, not a retry's.
func Classify(err error, host string) (kind, advice string) {
	if err == nil {
		return "", ""
	}
	msg := strings.ToLower(err.Error())
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	switch {
	case errors.As(err, &certErr), errors.As(err, &unknownAuth), errors.As(err, &hostErr),
		strings.Contains(msg, "x509"), strings.Contains(msg, "certificate"):
		return ProblemTLS, "The server's certificate is not trusted for " + host +
			". Check the server name — it must match the certificate — or ask whoever runs the server."
	case strings.Contains(msg, "starttls"), strings.Contains(msg, "plaintext"):
		return ProblemPlaintext, "The server does not offer encryption on this port, and InboxQL will not send a password unencrypted. " +
			"Try the encrypted port: 993 for IMAP, 465 for SMTP."
	case errors.As(err, &dnsErr), strings.Contains(msg, "no such host"):
		return ProblemHost, "There is no server called " + host + ". Check the spelling."
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"),
		strings.Contains(msg, "connection refused"), strings.Contains(msg, "unreachable"),
		strings.Contains(msg, "connection reset"), strings.Contains(msg, "eof"):
		return ProblemUnreachable, host + " did not answer on that port. Check the port, and that this computer can reach the server."
	case strings.Contains(msg, "authenticat"), strings.Contains(msg, "invalid credentials"),
		strings.Contains(msg, "login failed"), strings.Contains(msg, "password"),
		strings.Contains(msg, "auth"), strings.Contains(msg, "535"), strings.Contains(msg, "logon failure"):
		return ProblemAuth, "The server rejected the username or password." + authHint(host)
	default:
		return ProblemOther, "The connection failed: " + err.Error()
	}
}

// authHint names the usual cause for the providers where a correct password
// is still refused.
func authHint(host string) string {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "gmail") || strings.Contains(h, "google"):
		return " Gmail does not accept your Google password here: use an app password, or Sign in with Google."
	case strings.Contains(h, "icloud") || strings.Contains(h, "me.com"):
		return " iCloud needs an app-specific password, made at account.apple.com."
	case strings.Contains(h, "yahoo") || strings.Contains(h, "aol"):
		return " Yahoo and AOL need an app password, made in the account's security settings."
	case strings.Contains(h, "office365") || strings.Contains(h, "outlook"):
		return " Microsoft has turned off password sign-in for IMAP on Outlook.com and Microsoft 365."
	}
	return ""
}
