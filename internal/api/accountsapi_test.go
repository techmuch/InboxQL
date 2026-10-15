package api

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emersion/go-imap/backend/memory"
	imapserver "github.com/emersion/go-imap/server"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
)

// mailServers starts a real IMAP server (implicit TLS) and a real enough SMTP
// server (STARTTLS, AUTH PLAIN) on loopback, with a certificate the verify
// is told to trust. The login for both is username / password.
func mailServers(t *testing.T) (imapPort, smtpPort int, cert tls.Certificate) {
	t.Helper()
	https := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(https.Close)
	cert = https.TLS.Certificates[0]
	pool := x509.NewCertPool()
	pool.AddCert(https.Certificate())
	account.TestRoots = pool
	t.Cleanup(func() { account.TestRoots = nil })
	tlsConf := &tls.Config{Certificates: []tls.Certificate{cert}}

	srv := imapserver.New(memory.New())
	srv.AllowInsecureAuth = true
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConf)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	sl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sl.Close() })
	go func() {
		for {
			c, err := sl.Accept()
			if err != nil {
				return
			}
			go fakeSMTP(c, tlsConf)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, sl.Addr().(*net.TCPAddr).Port, cert
}

func fakeSMTP(c net.Conn, conf *tls.Config) {
	defer c.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
	say := func(s string) { rw.WriteString(s + "\r\n"); rw.Flush() }
	say("220 fake ESMTP")
	secure := false
	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if secure {
				say("250-fake\r\n250 AUTH PLAIN")
			} else {
				say("250-fake\r\n250 STARTTLS")
			}
		case cmd == "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(c, conf)
			if tc.Handshake() != nil {
				return
			}
			c, secure = tc, true
			rw = bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line)[len("AUTH PLAIN "):])
			if string(raw) == "\x00username\x00password" {
				say("235 ok")
			} else {
				say("535 5.7.8 Authentication credentials invalid")
			}
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("502 no")
		}
	}
}

func TestVerifyTalksToTheServers(t *testing.T) {
	setupTestDB(t)
	imapPort, smtpPort, _ := mailServers(t)
	mux := http.NewServeMux()
	registerAccountRoutes(mux)
	form := func(pass string) map[string]any {
		return map[string]any{"host": "127.0.0.1", "port": imapPort, "ssl": true, "user": "username", "password": pass,
			"smtpHost": "127.0.0.1", "smtpPort": smtpPort}
	}
	verdict := func(out map[string]any, server string) map[string]any { return out[server].(map[string]any) }

	_, out := call(t, mux, "POST", "/api/accounts/verify", form("password"))
	if verdict(out, "imap")["ok"] != true || verdict(out, "smtp")["ok"] != true {
		t.Fatalf("right password: %v", out)
	}

	_, out = call(t, mux, "POST", "/api/accounts/verify", form("wrong"))
	if verdict(out, "imap")["kind"] != account.ProblemAuth || verdict(out, "smtp")["kind"] != account.ProblemAuth {
		t.Fatalf("wrong password: %v", out)
	}

	// Not a simulation any more: a host that does not exist is reported as such.
	bad := form("password")
	bad["host"], bad["smtpHost"] = "nosuch.invalid", ""
	_, out = call(t, mux, "POST", "/api/accounts/verify", bad)
	if verdict(out, "imap")["kind"] != account.ProblemHost || verdict(out, "smtp")["skipped"] != true {
		t.Fatalf("no such host: %v", out)
	}

	account.TestRoots = nil // the system does not trust the test certificate
	_, out = call(t, mux, "POST", "/api/accounts/verify", form("password"))
	if verdict(out, "imap")["kind"] != account.ProblemTLS {
		t.Fatalf("untrusted certificate: %v", out)
	}
}

func TestVerifyNeverLendsAStoredPasswordToANewServer(t *testing.T) {
	setupTestDB(t)
	imapPort, smtpPort, _ := mailServers(t)
	store.SaveAccount(&account.Account{ID: "w", Name: "Work", Host: "127.0.0.1", Port: imapPort, SSL: true,
		User: "username", Password: "password", SMTPHost: "127.0.0.1", SMTPPort: smtpPort})
	mux := http.NewServeMux()
	registerAccountRoutes(mux)

	// The saved account, as it is.
	_, out := call(t, mux, "POST", "/api/accounts/verify", map[string]any{"id": "w"})
	if out["imap"].(map[string]any)["ok"] != true {
		t.Fatalf("saved account: %v", out)
	}
	// Same servers, blank password: the stored one is used.
	same := map[string]any{"id": "w", "host": "127.0.0.1", "port": imapPort, "ssl": true, "user": "username",
		"smtpHost": "127.0.0.1", "smtpPort": smtpPort}
	if _, out := call(t, mux, "POST", "/api/accounts/verify", same); out["smtp"].(map[string]any)["ok"] != true {
		t.Fatalf("unchanged servers: %v", out)
	}
	// Only the outgoing server moved. It would have been sent the stored
	// incoming password; now there is no password to send.
	moved := map[string]any{"id": "w", "host": "127.0.0.1", "port": imapPort, "ssl": true, "user": "username",
		"smtpHost": "attacker.example", "smtpPort": 587}
	if rec, out := call(t, mux, "POST", "/api/accounts/verify", moved); rec.Code != 400 {
		t.Fatalf("moved SMTP host = %d %v", rec.Code, out)
	}
}

func TestDisconnectKeepsTheMail(t *testing.T) {
	setupTestDB(t)
	store.SaveAccount(&account.Account{ID: "old", Name: "Old job", Host: "imap.example.com", Port: 993, SSL: true,
		User: "me", Password: "secret", SMTPPassword: "other"})
	mux := http.NewServeMux()
	registerAccountRoutes(mux)
	if rec, out := call(t, mux, "POST", "/api/accounts/disconnect?id=old", nil); rec.Code != 200 || out["disconnected"] != true {
		t.Fatalf("disconnect = %d %v", rec.Code, out)
	}
	acc, _ := store.GetAccount("old")
	if acc == nil || acc.Password != "" || acc.SMTPPassword != "" || !acc.Disconnected {
		t.Fatalf("after disconnect: %+v", acc)
	}
	if rec, _ := call(t, mux, "POST", "/api/accounts/disconnect?id=nope", nil); rec.Code != 404 {
		t.Errorf("unknown account = %d", rec.Code)
	}
	if _, out := call(t, mux, "GET", "/api/accounts/footprint?id=old", nil); out["messages"] != float64(0) {
		t.Errorf("footprint %v", out)
	}
}
