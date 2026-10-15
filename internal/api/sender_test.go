package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
)

// sendingAccount is an account whose servers are the fakes.
func sendingAccount(t *testing.T) *account.Account {
	t.Helper()
	setupTestDB(t)
	imapPort, smtpPort, _ := mailServers(t)
	acc := &account.Account{ID: "me", Name: "Me", Email: "me@example.com", Host: "127.0.0.1", Port: imapPort,
		SSL: true, User: "username", Password: "password", SMTPHost: "127.0.0.1", SMTPPort: smtpPort}
	if err := store.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	smtpMu.Lock()
	smtpReceived = nil
	smtpMu.Unlock()
	return acc
}

func received() []string {
	smtpMu.Lock()
	defer smtpMu.Unlock()
	return append([]string(nil), smtpReceived...)
}

// Send waits out the Undo window, then the mail goes, a copy is kept under
// Sent, and the draft says it was sent.
func TestSendGoesAfterTheUndoWindow(t *testing.T) {
	sendingAccount(t)
	mux := http.NewServeMux()
	registerDraftRoutes(mux)
	defer func(w time.Duration) { UndoWindow = w }(UndoWindow)
	UndoWindow = 50 * time.Millisecond

	status, out := call(t, mux, "POST", "/api/drafts", map[string]any{
		"accountId": "me", "to": []string{"ada@example.com"}, "subject": "Hello", "body": "Hi Ada", "send": true,
	})
	if status.Code != http.StatusCreated {
		t.Fatalf("create: %d %v", status.Code, out)
	}
	id := out["draft"].(map[string]any)["id"].(string)

	sendDue() // too early: still inside the window
	if len(received()) != 0 {
		t.Fatal("sent before the Undo window passed")
	}
	time.Sleep(60 * time.Millisecond)
	sendDue()
	got := received()
	if len(got) != 1 || !strings.Contains(got[0], "Subject: Hello") || !strings.Contains(got[0], "Message-ID: <") {
		t.Fatalf("the server received %q", got)
	}
	d, _ := store.GetDraft(id)
	if d.Status != store.DraftStatusSent || d.SentAt == nil {
		t.Errorf("draft is %s, want sent", d.Status)
	}
	// Kept here under Sent, read.
	res, err := store.RunQuery("folder:sent", 10, 0)
	if err != nil || len(res.Messages) != 1 || res.Messages[0].Subject != "Hello" {
		t.Fatalf("Sent holds %v (%v)", res, err)
	}
	// Sending twice is impossible.
	sendDue()
	if len(received()) != 1 {
		t.Error("the message went twice")
	}
}

func TestUndoTakesItBack(t *testing.T) {
	sendingAccount(t)
	mux := http.NewServeMux()
	registerDraftRoutes(mux)

	_, out := call(t, mux, "POST", "/api/drafts", map[string]any{
		"accountId": "me", "to": []string{"ada@example.com"}, "subject": "Oops", "body": "", "send": true,
	})
	id := out["draft"].(map[string]any)["id"].(string)
	if status, _ := call(t, mux, "POST", "/api/drafts/"+id+"/undo", nil); status.Code != http.StatusOK {
		t.Fatalf("undo: %d", status.Code)
	}
	d, _ := store.GetDraft(id)
	if d.Status != store.DraftStatusDraft || d.SendAt != nil {
		t.Errorf("after undo the draft is %s, sendAt %v", d.Status, d.SendAt)
	}
	sendDue()
	if len(received()) != 0 {
		t.Error("an undone message was sent")
	}

	// Sent from the draft, and gone: too late to undo.
	defer func(w time.Duration) { UndoWindow = w }(UndoWindow)
	UndoWindow = 0
	if status, out := call(t, mux, "POST", "/api/drafts/"+id+"/send", nil); status.Code != http.StatusOK {
		t.Fatalf("send: %d %v", status.Code, out)
	}
	sendDue()
	if status, _ := call(t, mux, "POST", "/api/drafts/"+id+"/undo", nil); status.Code != http.StatusConflict {
		t.Errorf("undo after sending = %d, want 409", status.Code)
	}
}

// A failure is said, and the draft is kept to try again.
func TestSendFailureKeepsTheDraft(t *testing.T) {
	acc := sendingAccount(t)
	acc.SMTPPassword = "wrong"
	acc.SMTPUser = "username"
	if err := store.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerDraftRoutes(mux)
	defer func(w time.Duration) { UndoWindow = w }(UndoWindow)
	UndoWindow = 0
	_, out := call(t, mux, "POST", "/api/drafts", map[string]any{
		"accountId": "me", "to": []string{"ada@example.com"}, "subject": "Nope", "body": "", "send": true,
	})
	id := out["draft"].(map[string]any)["id"].(string)
	sendDue()
	d, _ := store.GetDraft(id)
	if d.Status != store.DraftStatusFailed || d.LastError == "" {
		t.Errorf("draft is %s (%q), want failed with the reason", d.Status, d.LastError)
	}
}
