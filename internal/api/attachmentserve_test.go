package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// serveFixture stands up the API over a mailbox holding two files: an
// innocuous PDF, and an HTML document someone mailed.
type serveFixture struct {
	handler http.Handler
	pdfKey  string
	htmlKey string
}

func newServeFixture(t *testing.T) serveFixture {
	t.Helper()

	dir := t.TempDir()
	if _, err := store.InitDB(dir); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(store.CloseDB)

	auth.SetTrustLocal(true)
	t.Cleanup(func() { auth.SetTrustLocal(false) })
	if err := auth.CreateInitialUser("admin@inboxql.local", "testpass123"); err != nil {
		t.Fatalf("CreateInitialUser: %v", err)
	}

	SetDataDir(dir)
	blobs := blobstore.New(dir)

	if err := store.SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	msg := &message.Message{
		ID: "m1", AccountID: "acct", MessageID: "<m1@acme.com>",
		From: "alice@acme.com", To: []string{"me@example.com"},
		Subject: "Files", Body: "Attached.",
		Date: time.Now(), ContentHash: "ch1",
		Header: []byte("Message-ID: <m1@acme.com>\r\n"),
	}
	if err := store.SaveMessage(msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	put := func(id, filename, mimeType string, data []byte) string {
		t.Helper()
		hash, err := blobs.Put(data)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := store.SaveAttachment(&store.Attachment{
			ID: id, MessageID: "m1", Filename: filename, MimeType: mimeType,
			Size: int64(len(data)), ContentHash: hash, StoragePath: blobs.Path(hash),
		}); err != nil {
			t.Fatalf("SaveAttachment: %v", err)
		}
		return hash
	}

	f := serveFixture{
		pdfKey: put("a1", "invoice.pdf", "application/pdf", []byte("%PDF-1.4 pretend")),
		htmlKey: put("a2", "note.html", "text/html",
			[]byte("<script>alert(document.cookie)</script>")),
	}

	h, err := Router()
	if err != nil {
		t.Fatalf("Router: %v", err)
	}
	f.handler = h
	return f
}

func (f serveFixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}

// Every response carrying mailed bytes wears the same armour, whatever the
// bytes turn out to be.
func TestServedBytesAlwaysCarryTheHeaders(t *testing.T) {
	f := newServeFixture(t)

	for _, key := range []string{f.pdfKey, f.htmlKey} {
		rec := f.get(t, "/api/attachments/content?key="+key)
		if rec.Code != http.StatusOK {
			t.Fatalf("key %s: status %d", key[:8], rec.Code)
		}
		h := rec.Header()
		if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("key %s: X-Content-Type-Options %q, want nosniff", key[:8], got)
		}
		if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
			t.Errorf("key %s: CSP %q, want a sandbox", key[:8], csp)
		}
		if got := h.Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
			t.Errorf("key %s: CORP %q, want same-origin", key[:8], got)
		}
		if h.Get("Content-Disposition") == "" {
			t.Errorf("key %s: no Content-Disposition at all", key[:8])
		}
	}
}

// The headline case: a mailed HTML file must never be described to the browser
// as HTML, and must never be told to render.
func TestMailedHTMLIsNeverRendered(t *testing.T) {
	f := newServeFixture(t)

	// Even when the request explicitly asks for it.
	rec := f.get(t, "/api/attachments/content?key="+f.htmlKey+"&disposition=inline")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type %q, want application/octet-stream — a stranger's HTML is not HTML here", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition %q, want attachment despite disposition=inline", got)
	}

	// The bytes themselves are served intact; it is the framing that changes.
	if !strings.Contains(rec.Body.String(), "<script>") {
		t.Error("the file's bytes were altered; only the headers should differ")
	}
}

// A PDF previews, because that is the feature — but only when asked, and still
// under the sandbox.
func TestPDFPreviewsOnlyWhenAsked(t *testing.T) {
	f := newServeFixture(t)

	rec := f.get(t, "/api/attachments/content?key="+f.pdfKey)
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("without disposition=inline: %q, want a download by default", got)
	}

	rec = f.get(t, "/api/attachments/content?key="+f.pdfKey+"&disposition=inline")
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type %q, want application/pdf", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("Content-Disposition %q, want inline", got)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Error("an inline preview lost its sandbox")
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "invoice.pdf") {
		t.Errorf("Content-Disposition %q lost the filename", got)
	}
}

// The key is a path component. Anything that is not a content address is a
// traversal attempt, and gets the same answer as a file that is simply absent.
func TestContentRejectsKeysThatAreNotAddresses(t *testing.T) {
	f := newServeFixture(t)

	for _, key := range []string{
		"", "..", "../../../etc/passwd", "%2e%2e%2f%2e%2e%2fetc%2fpasswd",
		"deadbeef", strings.Repeat("a", 64), strings.Repeat("z", 64),
	} {
		rec := f.get(t, "/api/attachments/content?key="+key)
		if rec.Code != http.StatusNotFound {
			t.Errorf("key %q: status %d, want 404", key, rec.Code)
		}
		if rec.Body.Len() > 0 && strings.Contains(rec.Body.String(), "root:") {
			t.Fatalf("key %q served something that looks like /etc/passwd", key)
		}
	}
}

// Bytes on disk that the database has no row for are not served. Otherwise a
// blob outliving its message stays readable to anyone who kept the hash.
func TestUnreferencedBlobIsNotServed(t *testing.T) {
	f := newServeFixture(t)

	// Bytes in the store that no attachment row points at — what a deleted
	// message leaves behind until a sweep reclaims it.
	orphan, err := blobstore.New(importDataDir).Put([]byte("orphaned bytes"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rec := f.get(t, "/api/attachments/content?key="+orphan)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 — the database is the authority on what exists", rec.Code)
	}
}

// The metadata endpoints are the other half of the surface: a file by key, and
// the messages it arrived on.
func TestAttachmentMetadataEndpoints(t *testing.T) {
	f := newServeFixture(t)

	rec := f.get(t, "/api/attachments/file?key="+f.pdfKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("file: status %d", rec.Code)
	}
	var file store.AttachmentFile
	if err := json.Unmarshal(rec.Body.Bytes(), &file); err != nil {
		t.Fatalf("decoding file: %v", err)
	}
	if file.Filename != "invoice.pdf" || file.Messages != 1 {
		t.Errorf("file = %+v, want invoice.pdf on 1 message", file)
	}

	rec = f.get(t, "/api/attachments/occurrences?key="+f.pdfKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("occurrences: status %d", rec.Code)
	}
	var occurrences []store.AttachmentOccurrence
	if err := json.Unmarshal(rec.Body.Bytes(), &occurrences); err != nil {
		t.Fatalf("decoding occurrences: %v", err)
	}
	if len(occurrences) != 1 || occurrences[0].MessageID != "m1" {
		t.Errorf("occurrences = %+v, want one on m1", occurrences)
	}
	if occurrences[0].Subject != "Files" {
		t.Errorf("occurrence subject %q, want Files", occurrences[0].Subject)
	}
}

// Unauthenticated requests get nothing, the same as every other route.
func TestAttachmentContentRequiresAuth(t *testing.T) {
	f := newServeFixture(t)

	auth.SetTrustLocal(false)
	r := httptest.NewRequest(http.MethodGet, "/api/attachments/content?key="+f.pdfKey, nil)
	r.RemoteAddr = "203.0.113.9:54321"
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)

	if rec.Code == http.StatusOK {
		t.Errorf("an unauthenticated request was served %d bytes of a private file", rec.Body.Len())
	}
}
