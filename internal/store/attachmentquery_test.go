package store

import (
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/message"
)

// openFileQueryFixture builds a mailbox whose files exercise the awkward cases:
// one document on two messages in two different threads, the same bytes under a
// second name, a file only one person sent, and an inline image.
func openFileQueryFixture(t *testing.T) *blobstore.Store {
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

	type fixture struct {
		id       string
		from     string
		subject  string
		day      int
		filename string
		payload  string
		mime     string
		inline   bool
	}
	// The invoice arrives twice: once from Alice, once forwarded by Bob under a
	// different name. Same bytes, so it is one file that reached two people.
	// Sizes are spread far enough apart that a threshold test means something:
	// close-together payloads would make `size>` pass or fail on a byte of
	// boundary text rather than on the behaviour being checked.
	big := strings.Repeat("invoice bytes ", 200) // ~2800
	fixtures := []fixture{
		{"m1", "alice@acme.com", "Invoice attached", 1, "invoice.pdf", "%PDF-1.4 " + big, "application/pdf", false},
		{"m2", "bob@acme.com", "Fwd: Invoice attached", 2, "invoice-copy.pdf", "%PDF-1.4 " + big, "application/pdf", false},
		{"m3", "alice@acme.com", "Photos", 3, "beach.png", "\x89PNG " + strings.Repeat("beach", 40), "image/png", false},
		{"m4", "carol@other.org", "Signature", 4, "logo.png", "\x89PNG logo", "image/png", true},
		{"m5", "carol@other.org", "Spreadsheet", 5, "budget.xlsx", "budget bytes",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", false},
	}

	for _, f := range fixtures {
		disposition := "attachment"
		if f.inline {
			disposition = "inline"
		}
		raw := []byte("MIME-Version: 1.0\r\n" +
			"Message-ID: <" + f.id + "@example.test>\r\n" +
			"Subject: " + f.subject + "\r\n" +
			"From: " + f.from + "\r\n" +
			"To: me@example.com\r\n" +
			"Content-Type: multipart/mixed; boundary=\"sep\"\r\n\r\n" +
			"--sep\r\nContent-Type: text/plain\r\n\r\nBody text.\r\n" +
			"--sep\r\nContent-Type: " + f.mime + "\r\n" +
			"Content-Disposition: " + disposition + "; filename=\"" + f.filename + "\"\r\n\r\n" +
			f.payload + "\r\n--sep--\r\n")

		msg, err := message.ParseRFC822(raw)
		if err != nil {
			t.Fatalf("ParseRFC822 %s: %v", f.id, err)
		}
		msg.ID = f.id
		msg.AccountID = "acct"
		msg.ContentHash = f.id
		msg.Mailbox = "INBOX"
		msg.From = f.from
		msg.To = []string{"me@example.com"}
		msg.Subject = f.subject
		msg.Date = time.Date(2026, 3, f.day, 12, 0, 0, 0, time.Local)
		msg.InternalDate = msg.Date
		msg.Size = uint32(len(raw))
		msg.Header = raw
		if err := SaveMessage(msg); err != nil {
			t.Fatalf("SaveMessage %s: %v", f.id, err)
		}
		if _, err := StoreAttachments(f.id, raw, blobs, 0); err != nil {
			t.Fatalf("StoreAttachments %s: %v", f.id, err)
		}
	}

	return blobs
}

func fileNames(files []*AttachmentFile) map[string]*AttachmentFile {
	out := map[string]*AttachmentFile{}
	for _, f := range files {
		out[f.Filename] = f
	}
	return out
}

// The whole premise of the entity: a list of files, not a list of arrivals.
func TestAttachmentEntityListsFilesNotOccurrences(t *testing.T) {
	openFileQueryFixture(t)

	res, err := RunQuery("in:attachments", 0, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if res.Kind != "attachments" {
		t.Fatalf("kind %q, want attachments", res.Kind)
	}
	// Five arrivals, four distinct files: the invoice arrived twice.
	if len(res.Attachments) != 4 {
		names := []string{}
		for _, f := range res.Attachments {
			names = append(names, f.Filename)
		}
		t.Fatalf("listed %d files %v, want 4", len(res.Attachments), names)
	}

	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM attachments").Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 5 {
		t.Fatalf("fixture stored %d attachment rows, want 5", rows)
	}

	// The newest arrival names the file, so the invoice shows Bob's name for it.
	byName := fileNames(res.Attachments)
	invoice, ok := byName["invoice-copy.pdf"]
	if !ok {
		t.Fatalf("the invoice is not listed under its most recent name: %v", byName)
	}
	if invoice.Messages != 2 {
		t.Errorf("invoice reached %d messages, want 2", invoice.Messages)
	}
	if invoice.Threads != 2 {
		t.Errorf("invoice spans %d threads, want 2", invoice.Threads)
	}
	if invoice.Names != 2 {
		t.Errorf("invoice arrived under %d names, want 2", invoice.Names)
	}
	if !invoice.Shared() {
		t.Error("invoice does not report itself as shared")
	}
}

// A predicate about mail asks about every message the file arrived on, because
// the entity is the file. The invoice came from Alice and from Bob, and asking
// for either has to find it.
func TestAttachmentQueryReachesThroughToMail(t *testing.T) {
	openFileQueryFixture(t)

	for _, sender := range []string{"alice@acme.com", "bob@acme.com"} {
		res, err := RunQuery("in:attachments from:"+sender, 0, 0)
		if err != nil {
			t.Fatalf("from:%s: %v", sender, err)
		}
		if _, ok := fileNames(res.Attachments)["invoice-copy.pdf"]; !ok {
			t.Errorf("from:%s did not find the invoice it carried", sender)
		}
	}

	// Alice sent the invoice and the beach photo, nothing else.
	res, err := RunQuery("in:attachments from:alice", 0, 0)
	if err != nil {
		t.Fatalf("from:alice: %v", err)
	}
	if len(res.Attachments) != 2 {
		t.Errorf("from:alice matched %d files, want 2", len(res.Attachments))
	}

	// A file matched through one of its messages still reports its full reach.
	// Anything else would make the same file's count depend on how it was
	// found, which is how a listing quietly lies.
	if f, ok := fileNames(res.Attachments)["invoice-copy.pdf"]; ok && f.Messages != 2 {
		t.Errorf("invoice found via from:alice reports %d messages, want its true 2", f.Messages)
	}

	res, err = RunQuery("in:attachments after:2026-03-04", 0, 0)
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	if len(res.Attachments) != 2 {
		t.Errorf("after:2026-03-04 matched %d files, want 2 (logo, budget)", len(res.Attachments))
	}
}

func TestAttachmentFilters(t *testing.T) {
	openFileQueryFixture(t)

	cases := []struct {
		query string
		want  int
		note  string
	}{
		{"in:attachments type:pdf", 1, "one distinct PDF, arrived twice"},
		{"in:attachments type:image", 2, "beach and logo"},
		{"in:attachments type:sheet", 1, "the xlsx, by category not MIME string"},
		{"in:attachments type:xlsx", 1, "by extension when the category is not used"},
		{"in:attachments filename:*.png", 2, "glob over the name"},
		{"in:attachments filename:invoice", 1, "substring finds either of its names"},
		{"in:attachments is:shared", 1, "only the invoice went to two messages"},
		{"in:attachments -is:shared", 3, "the other three did not"},
		{"in:attachments is:inline", 1, "the signature logo"},
		{"in:attachments is:attached", 3, "everything else"},
		{"in:attachments is:stored", 4, "all bytes were kept"},
		{"in:attachments is:missing", 0, "so none are missing"},
		{"in:attachments messages>1", 1, "the invoice again, by count"},
		{"in:attachments size>1kb", 1, "only the invoice clears a kilobyte"},
		{"in:attachments size<1kb", 3, "the other three are small"},
		{"in:attachments beach", 1, "a bare word finds the filename"},
		{"in:attachments Spreadsheet", 1, "and a bare word finds the mail's text"},
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

// A filename that varies between arrivals must not split the file in two, and
// must not drop the arrivals that used the other name — that would report a
// file that went to two people as having reached one.
func TestAttachmentFilterKeepsTheWholeFile(t *testing.T) {
	openFileQueryFixture(t)

	// Match on the name only the first arrival used.
	res, err := RunQuery("in:attachments filename:=invoice.pdf", 0, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Attachments) != 1 {
		t.Fatalf("matched %d files, want 1", len(res.Attachments))
	}
	if got := res.Attachments[0].Messages; got != 2 {
		t.Errorf("file matched by one of its names reports %d messages, want 2", got)
	}
	if got := res.Attachments[0].Names; got != 2 {
		t.Errorf("file reports %d names, want 2", got)
	}
}

func TestAttachmentOrdering(t *testing.T) {
	openFileQueryFixture(t)

	first := func(q string) *AttachmentFile {
		t.Helper()
		res, err := RunQuery(q, 0, 0)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if len(res.Attachments) == 0 {
			t.Fatalf("%s returned nothing", q)
		}
		return res.Attachments[0]
	}

	// Default is newest first, like mail.
	if got := first("in:attachments").Filename; got != "budget.xlsx" {
		t.Errorf("default order leads with %q, want budget.xlsx", got)
	}
	if got := first("in:attachments | sort size desc").Filename; got != "invoice-copy.pdf" {
		t.Errorf("sort size desc leads with %q, want the invoice", got)
	}
	if got := first("in:attachments | sort name asc").Filename; got != "beach.png" {
		t.Errorf("sort name asc leads with %q, want beach.png", got)
	}
	if got := first("in:attachments | sort messages desc").Filename; got != "invoice-copy.pdf" {
		t.Errorf("sort messages desc leads with %q, want the invoice", got)
	}

	if _, err := RunQuery("in:attachments | sort colour", 0, 0); err == nil {
		t.Error("sorting by a field files do not have was accepted")
	}
}

func TestAttachmentAggregates(t *testing.T) {
	openFileQueryFixture(t)

	res, err := RunQuery("in:attachments | count", 0, 0)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	// Files, not arrivals: five rows in the table, four files.
	if res.Total != 4 {
		t.Errorf("count returned %d, want 4 files", res.Total)
	}

	res, err = RunQuery("in:attachments | top extension", 0, 0)
	if err != nil {
		t.Fatalf("top extension: %v", err)
	}
	got := map[string]float64{}
	for _, g := range res.Groups {
		got[g.Label] = g.Value
	}
	if got["png"] != 2 || got["pdf"] != 1 || got["xlsx"] != 1 {
		t.Errorf("extension groups %v, want png:2 pdf:1 xlsx:1", got)
	}

	res, err = RunQuery("in:attachments | top from", 0, 0)
	if err != nil {
		t.Fatalf("top from: %v", err)
	}
	if len(res.Groups) == 0 {
		t.Error("grouping files by sender produced nothing")
	}
}

// `in:files` is the same entity. Someone who thinks of them as files should not
// have to learn that the table is called attachments.
func TestAttachmentEntityAliases(t *testing.T) {
	openFileQueryFixture(t)

	for _, q := range []string{"in:attachments", "in:attachment", "in:files", "in:file"} {
		res, err := RunQuery(q, 0, 0)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if res.Kind != "attachments" || len(res.Attachments) != 4 {
			t.Errorf("%s gave kind %q with %d rows, want attachments/4", q, res.Kind, len(res.Attachments))
		}
	}
}

// `size` means the message in a mail query and the file in a file query. The
// same word answering two different questions is only safe if each query gets
// the one it asked for.
func TestAttachmentSizeIsTheFileNotTheMessage(t *testing.T) {
	openFileQueryFixture(t)

	// A message is always bigger than the file it carries — it wraps that file
	// in a MIME envelope and a body. So there is a threshold above every file
	// and below the messages that carried the largest of them, and it is what
	// tells the two readings of `size` apart: no file clears it, two messages
	// do. Read the message's size, and the first assertion would match.
	const between = 3000

	files, err := RunQuery("in:attachments size>3000", 0, 0)
	if err != nil {
		t.Fatalf("file size: %v", err)
	}
	if len(files.Attachments) != 0 {
		t.Errorf("in:attachments size>%d matched %d files, want 0 — no file is that big",
			between, len(files.Attachments))
	}

	mail, err := RunQuery("larger:3000", 0, 0)
	if err != nil {
		t.Fatalf("message size: %v", err)
	}
	if len(mail.Messages) != 2 {
		t.Errorf("larger:%d matched %d messages, want the 2 carrying the invoice",
			between, len(mail.Messages))
	}
}

// An unknown file field must say so rather than silently asking about mail.
func TestAttachmentUnknownFieldErrors(t *testing.T) {
	openFileQueryFixture(t)

	if _, err := RunQuery("in:attachments is:sparkly", 0, 0); err == nil {
		t.Error("is:sparkly was accepted as a file state")
	}
	if _, err := RunQuery("in:attachments has:wings", 0, 0); err == nil {
		t.Error("has:wings was accepted")
	}
}
