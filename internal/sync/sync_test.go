package sync

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	imapserver "github.com/emersion/go-imap/server"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
)

// imapServer starts a real IMAP server over TLS on loopback. The memory
// backend's INBOX starts with one message whose UID is 6, so UIDs and
// sequence numbers already disagree — as they do on every real mailbox that
// has ever deleted or archived anything.
func imapServer(t *testing.T) *account.Account {
	t.Helper()
	t.Setenv("INBOXQL_HOME", t.TempDir())
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	https := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(https.Close)
	pool := x509.NewCertPool()
	pool.AddCert(https.Certificate())
	account.TestRoots = pool
	t.Cleanup(func() { account.TestRoots = nil })

	srv := imapserver.New(memory.New())
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: https.TLS.Certificates})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	acc := &account.Account{ID: t.Name(), Name: t.Name(), Host: "127.0.0.1",
		Port: ln.Addr().(*net.TCPAddr).Port, SSL: true, User: "username", Password: "password"}
	if err := store.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	return acc
}

// deliver appends a message to INBOX, as new mail arriving.
func deliver(t *testing.T, acc *account.Account, id string) {
	t.Helper()
	c, err := ConnectIMAP(acc)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Logout()
	msg := fmt.Sprintf("From: a@example.com\r\nTo: b@example.com\r\nSubject: %s\r\nMessage-ID: <%s@example.com>\r\nDate: %s\r\n\r\nbody\r\n",
		id, id, time.Now().Format(time.RFC1123Z))
	if err := c.Append("INBOX", nil, time.Now(), bytes.NewBufferString(msg)); err != nil {
		t.Fatal(err)
	}
}

func inboxCount(t *testing.T, acc *account.Account) int {
	t.Helper()
	stats, err := store.GetAccountStats(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return stats.TotalMessages
}

// TestSyncPicksUpNewMail is the bug a real Gmail account showed: the first
// sync worked, and every later one reported success while fetching nothing,
// because the next UID was compared with the message count and fetched as a
// sequence number.
func TestSyncPicksUpNewMail(t *testing.T) {
	acc := imapServer(t)
	sm := NewSyncManager(1)

	sm.StartSync(acc)
	if n := inboxCount(t, acc); n != 1 {
		t.Fatalf("first sync stored %d messages, want 1", n)
	}

	deliver(t, acc, "second")
	deliver(t, acc, "third")
	sm.StartSync(acc)
	if n := inboxCount(t, acc); n != 3 {
		t.Fatalf("after new mail arrived, sync stored %d messages in all, want 3", n)
	}

	// Nothing new: a sync fetches nothing and stores nothing twice.
	sm.StartSync(acc)
	if n := inboxCount(t, acc); n != 3 {
		t.Fatalf("an idle sync changed the count to %d, want 3", n)
	}
}

// TestSyncAfterUIDsReset covers a server that renumbers a folder (a new
// UIDVALIDITY): the saved position is past every UID it now has, and the sync
// must start again from the beginning rather than wait for UIDs to catch up.
func TestSyncAfterUIDsReset(t *testing.T) {
	acc := imapServer(t)
	sm := NewSyncManager(1)
	if err := store.SaveMailboxSyncState(&store.MailboxSyncState{
		ID: acc.ID + "-INBOX", AccountID: acc.ID, Name: "INBOX", LastUID: 140862,
	}); err != nil {
		t.Fatal(err)
	}
	deliver(t, acc, "after-reset")
	sm.StartSync(acc)
	if n := inboxCount(t, acc); n != 2 {
		t.Fatalf("stored %d messages, want 2", n)
	}
}
