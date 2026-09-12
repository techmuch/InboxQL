package store

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/filetext"
	"github.com/user/inboxql/internal/message"
)

// openTextFixture stores three files: a readable PDF, a scanned one with no
// text layer, and a JPEG nothing here can read. The three outcomes are the
// point — they call for different responses and must stay distinguishable.
func openTextFixture(t *testing.T) *blobstore.Store {
	t.Helper()

	dir := t.TempDir()
	if _, err := InitDB(dir); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)

	if err := SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	blobs := blobstore.New(dir)

	msg := &message.Message{
		ID: "m1", AccountID: "acct", MessageID: "<m1@acme.com>",
		From: "alice@acme.com", To: []string{"me@example.com"},
		Subject: "Paperwork", Body: "Attached.",
		Date: time.Now(), ContentHash: "ch1", Mailbox: "INBOX",
		Header: []byte("Message-ID: <m1@acme.com>\r\n"),
	}
	if err := SaveMessage(msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	put := func(id, filename, mime string, data []byte) {
		t.Helper()
		hash, err := blobs.Put(data)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := SaveAttachment(&Attachment{
			ID: id, MessageID: "m1", Filename: filename, MimeType: mime,
			Size: int64(len(data)), ContentHash: hash, StoragePath: blobs.Path(hash),
		}); err != nil {
			t.Fatalf("SaveAttachment: %v", err)
		}
	}

	put("a1", "invoice.pdf", "application/pdf", testPDF(t, "Invoice 4815 for landscaping"))
	put("a2", "scan.pdf", "application/pdf", testPDF(t, ""))
	put("a3", "photo.jpg", "image/jpeg", []byte("\xFF\xD8\xFF\xE0 not really a jpeg"))

	return blobs
}

func TestExtractAttachmentTextRecordsEveryOutcome(t *testing.T) {
	blobs := openTextFixture(t)

	got, err := ExtractAttachmentText(blobs, false, nil)
	if err != nil {
		t.Fatalf("ExtractAttachmentText: %v", err)
	}

	if got.Files != 3 {
		t.Errorf("saw %d files, want 3", got.Files)
	}
	if got.WithText != 1 {
		t.Errorf("%d files with text, want 1", got.WithText)
	}
	// A scan is not a failure. Conflating them would make the set OCR exists
	// for unnameable, and would have every later pass retry every image.
	if got.NoText != 1 {
		t.Errorf("%d files with no text layer, want 1", got.NoText)
	}
	if got.NoReader != 1 {
		t.Errorf("%d files with no reader, want 1", got.NoReader)
	}
	if got.Failed != 0 {
		t.Errorf("%d failures, want 0 — nothing here is broken", got.Failed)
	}
	if got.Pending != 0 {
		t.Errorf("%d files still unread after a full pass", got.Pending)
	}
}

// A second pass must not redo work, or extraction becomes O(mailbox) every
// time anything is imported.
func TestExtractAttachmentTextSkipsWhatItHasRead(t *testing.T) {
	blobs := openTextFixture(t)

	if _, err := ExtractAttachmentText(blobs, false, nil); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	var reread int
	if _, err := ExtractAttachmentText(blobs, false, func(done, total int64) {
		reread = int(total)
	}); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if reread != 0 {
		t.Errorf("second pass re-read %d files, want 0", reread)
	}

	// --redo exists precisely so a better extractor can reach the files that
	// motivated it.
	var redone int
	if _, err := ExtractAttachmentText(blobs, true, func(done, total int64) {
		redone = int(total)
	}); err != nil {
		t.Fatalf("redo pass: %v", err)
	}
	if redone != 3 {
		t.Errorf("redo re-read %d files, want 3", redone)
	}
}

func TestAttachmentTextIsStoredPerPage(t *testing.T) {
	blobs := openTextFixture(t)
	if _, err := ExtractAttachmentText(blobs, false, nil); err != nil {
		t.Fatalf("extract: %v", err)
	}

	var hash string
	if err := db.QueryRow(
		"SELECT content_hash FROM attachments WHERE filename = 'invoice.pdf'").Scan(&hash); err != nil {
		t.Fatalf("finding the invoice: %v", err)
	}

	pages, err := GetAttachmentText(hash)
	if err != nil {
		t.Fatalf("GetAttachmentText: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	if pages[0].Number != 1 {
		t.Errorf("page number %d, want 1", pages[0].Number)
	}

	extraction, err := GetAttachmentExtraction(hash)
	if err != nil || extraction == nil {
		t.Fatalf("GetAttachmentExtraction: %v, %v", extraction, err)
	}
	if extraction.Status != string(filetext.StatusOK) {
		t.Errorf("status %q, want ok", extraction.Status)
	}
	if extraction.Characters == 0 {
		t.Error("extraction recorded no characters")
	}
}

// The set OCR exists for has to be nameable, both from Go and from a query.
func TestScannedAttachmentsAreNameable(t *testing.T) {
	blobs := openTextFixture(t)
	if _, err := ExtractAttachmentText(blobs, false, nil); err != nil {
		t.Fatalf("extract: %v", err)
	}

	scanned, err := ScannedAttachments()
	if err != nil {
		t.Fatalf("ScannedAttachments: %v", err)
	}
	if len(scanned) != 1 {
		t.Fatalf("found %d scanned files, want 1", len(scanned))
	}

	res, err := RunQuery("in:attachments is:scanned", 0, 0)
	if err != nil {
		t.Fatalf("is:scanned: %v", err)
	}
	if len(res.Attachments) != 1 || res.Attachments[0].Filename != "scan.pdf" {
		t.Errorf("is:scanned matched %d files, want just scan.pdf", len(res.Attachments))
	}
}

func TestContentSearch(t *testing.T) {
	blobs := openTextFixture(t)
	if _, err := ExtractAttachmentText(blobs, false, nil); err != nil {
		t.Fatalf("extract: %v", err)
	}

	cases := []struct {
		query string
		want  int
		note  string
	}{
		{"in:attachments content:landscaping", 1, "a word only the file's text contains"},
		{"in:attachments content:4815", 1, "and a number"},
		{"in:attachments content:tractor", 0, "a word in no file"},
		{"in:attachments is:searchable", 1, "only one file yielded text"},
		{"in:attachments is:unread", 0, "everything has been read"},
		{"in:attachments has:text", 1, "the same set, said differently"},
		{"in:attachments -has:text", 2, "the scan and the photo"},
		// A bare word reaches the filename, the file's text, and the mail —
		// there is no way to tell which was meant, so it is all three.
		{"in:attachments landscaping", 1, "a bare word finds file contents"},
		{"in:attachments invoice", 1, "and still finds a filename"},
		{"in:attachments Paperwork", 3, "and still finds the mail's subject"},
	}

	for _, tc := range cases {
		res, err := RunQuery(tc.query, 0, 0)
		if err != nil {
			t.Errorf("%s: %v", tc.query, err)
			continue
		}
		if len(res.Attachments) != tc.want {
			names := []string{}
			for _, f := range res.Attachments {
				names = append(names, f.Filename)
			}
			t.Errorf("%s matched %d %v, want %d — %s",
				tc.query, len(res.Attachments), names, tc.want, tc.note)
		}
	}
}

// Re-extracting must replace the previous text rather than accumulate it, or a
// file re-read by a better extractor matches both readings forever.
func TestReExtractionReplacesText(t *testing.T) {
	openTextFixture(t)
	hash := "deadbeef"

	first := filetext.Result{
		Status: filetext.StatusOK, Extractor: "pdf",
		Pages: []filetext.Page{{Number: 1, Text: "the original reading"}},
	}
	if err := SaveAttachmentText(hash, first); err != nil {
		t.Fatalf("first save: %v", err)
	}

	second := filetext.Result{
		Status: filetext.StatusOK, Extractor: "ocr",
		Pages: []filetext.Page{{Number: 1, Text: "a better reading"}},
	}
	if err := SaveAttachmentText(hash, second); err != nil {
		t.Fatalf("second save: %v", err)
	}

	pages, err := GetAttachmentText(hash)
	if err != nil {
		t.Fatalf("GetAttachmentText: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("held %d pages after re-extraction, want 1", len(pages))
	}
	if pages[0].Text != "a better reading" {
		t.Errorf("text %q, want the newer reading", pages[0].Text)
	}

	extraction, _ := GetAttachmentExtraction(hash)
	if extraction == nil || extraction.Extractor != "ocr" {
		t.Errorf("extraction records %+v, want the newer extractor", extraction)
	}
}

// testPDF writes a minimal one-page PDF. Empty text gives a page with no text
// layer, which is what a scan looks like to an extractor.
func testPDF(t *testing.T, text string) []byte {
	t.Helper()

	content := "BT /F1 12 Tf 72 720 Td"
	if text != "" {
		content += fmt.Sprintf(" (%s) Tj", text)
	}
	content += " ET"

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	w.Write([]byte(content))
	w.Close()

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /Contents 4 0 R " +
		"/Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	fmt.Fprintf(&b, "4 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", compressed.Len())
	b.Write(compressed.Bytes())
	b.WriteString("\nendstream\nendobj\n")
	b.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica " +
		"/Encoding /WinAnsiEncoding >>\nendobj\n")
	b.WriteString("trailer\n<< /Root 1 0 R >>\n%%EOF\n")
	return b.Bytes()
}
