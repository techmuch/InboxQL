package store

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/message"
)

// rawWithAttachment builds a multipart message carrying one named part.
func rawWithAttachment(messageID, filename, body string) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	return []byte("MIME-Version: 1.0\r\n" +
		"Message-ID: <" + messageID + ">\r\n" +
		"Subject: Invoice\r\n" +
		"From: alice@acme.com\r\n" +
		"To: me@example.com\r\n" +
		"Content-Type: multipart/mixed; boundary=\"sep\"\r\n" +
		"\r\n" +
		"--sep\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"See attached.\r\n" +
		"--sep\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"" + filename + "\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" + encoded + "\r\n" +
		"--sep--\r\n")
}

// rawPlain is a message with nothing to extract.
func rawPlain(messageID string) []byte {
	return []byte("MIME-Version: 1.0\r\n" +
		"Message-ID: <" + messageID + ">\r\n" +
		"Subject: Lunch\r\n" +
		"From: bob@acme.com\r\n" +
		"To: me@example.com\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Free on Friday?\r\n")
}

// openAttachmentFixture stores three messages: two carrying the same file, and
// one carrying nothing.
func openAttachmentFixture(t *testing.T) *blobstore.Store {
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

	raws := map[string][]byte{
		"m1": rawWithAttachment("m1@acme.com", "invoice.pdf", "%PDF-1.4 fake invoice bytes"),
		// The same bytes arriving again, under a different name. Content
		// addressing means one blob, two rows.
		"m2": rawWithAttachment("m2@acme.com", "invoice-copy.pdf", "%PDF-1.4 fake invoice bytes"),
		"m3": rawPlain("m3@acme.com"),
	}

	for i, id := range []string{"m1", "m2", "m3"} {
		msg, err := message.ParseRFC822(raws[id])
		if err != nil {
			t.Fatalf("ParseRFC822 %s: %v", id, err)
		}
		msg.ID = id
		msg.AccountID = "acct"
		msg.ContentHash = id
		msg.Mailbox = "INBOX"
		msg.Date = time.Date(2026, 3, i+1, 12, 0, 0, 0, time.Local)
		msg.InternalDate = msg.Date
		// The recovery path reads raw MIME back out of this column, which is
		// exactly what the importer leaves there.
		msg.Header = raws[id]
		if err := SaveMessage(msg); err != nil {
			t.Fatalf("SaveMessage %s: %v", id, err)
		}
	}

	return blobstore.New(dir)
}

// Mail imported before extraction existed still has its parts in the stored raw
// message. That is the whole premise of the recovery command.
func TestRecoverAttachmentsFindsPartsInStoredMail(t *testing.T) {
	blobs := openAttachmentFixture(t)

	pending, err := UnextractedAttachments()
	if err != nil {
		t.Fatalf("UnextractedAttachments: %v", err)
	}
	if pending != 2 {
		t.Fatalf("doctor saw %d messages with unextracted attachments, want 2", pending)
	}

	out, err := RecoverAttachments(blobs, 0, false)
	if err != nil {
		t.Fatalf("RecoverAttachments: %v", err)
	}
	if out.Messages != 2 || out.Recovered != 2 || out.Stored != 2 {
		t.Errorf("recovered %d attachments from %d messages (%d stored), want 2/2/2",
			out.Recovered, out.Messages, out.Stored)
	}
	if out.Skipped != 0 {
		t.Errorf("skipped %d, want 0", out.Skipped)
	}

	// The plain message is not a candidate at all, so it is never even read.
	if out.Examined != 2 {
		t.Errorf("examined %d messages, want 2 — the plain one should not be a candidate", out.Examined)
	}

	for _, id := range []string{"m1", "m2"} {
		found, err := ListAttachments(id)
		if err != nil {
			t.Fatalf("ListAttachments %s: %v", id, err)
		}
		if len(found) != 1 {
			t.Fatalf("%s has %d attachments, want 1", id, len(found))
		}
		if !found[0].Stored() {
			t.Errorf("%s attachment has no storage path", id)
		}
		if found[0].MimeType != "application/pdf" {
			t.Errorf("%s attachment is %q, want application/pdf", id, found[0].MimeType)
		}
	}

	// Identical bytes under different filenames are one blob.
	var hashes int
	if err := db.QueryRow(
		"SELECT COUNT(DISTINCT content_hash) FROM attachments WHERE content_hash != ''").
		Scan(&hashes); err != nil {
		t.Fatalf("counting hashes: %v", err)
	}
	if hashes != 1 {
		t.Errorf("stored %d distinct blobs for identical bytes, want 1", hashes)
	}

	// Once recovered, doctor has nothing left to report.
	if pending, err = UnextractedAttachments(); err != nil || pending != 0 {
		t.Errorf("after recovery doctor reports %d pending (err %v), want 0", pending, err)
	}
}

// Running it twice must not double every attachment in the database — the
// realistic way to invoke this is "again, to be sure".
func TestRecoverAttachmentsIsIdempotent(t *testing.T) {
	blobs := openAttachmentFixture(t)

	if _, err := RecoverAttachments(blobs, 0, false); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := RecoverAttachments(blobs, 0, false)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Recovered != 0 || second.Messages != 0 {
		t.Errorf("second pass recovered %d attachments from %d messages, want 0/0",
			second.Recovered, second.Messages)
	}

	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM attachments").Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 2 {
		t.Errorf("database holds %d attachment rows after two passes, want 2", rows)
	}
}

// A dry run reports the same numbers and writes nothing.
func TestRecoverAttachmentsDryRunWritesNothing(t *testing.T) {
	blobs := openAttachmentFixture(t)

	out, err := RecoverAttachments(blobs, 0, true)
	if err != nil {
		t.Fatalf("RecoverAttachments: %v", err)
	}
	if !out.DryRun {
		t.Error("result does not report itself as a dry run")
	}
	if out.Recovered != 2 || out.Messages != 2 {
		t.Errorf("dry run reported %d attachments from %d messages, want 2/2",
			out.Recovered, out.Messages)
	}
	if out.Stored != 0 {
		t.Errorf("dry run stored %d attachments, want 0", out.Stored)
	}

	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM attachments").Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("dry run wrote %d attachment rows, want 0", rows)
	}

	// A dry run still reports a part's true size without decoding it.
	if out.Bytes == 0 {
		t.Error("dry run reported zero bytes; sizes should survive the one-byte cap")
	}
}

// StoreAttachments is what both ingestion paths call, so a second call for the
// same message — a re-save, a re-sync — must not duplicate its rows.
func TestStoreAttachmentsIsIdempotentPerMessage(t *testing.T) {
	blobs := openAttachmentFixture(t)
	raw := rawWithAttachment("m1@acme.com", "invoice.pdf", "%PDF-1.4 fake invoice bytes")

	first, err := StoreAttachments("m1", raw, blobs, 0)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Stored != 1 {
		t.Fatalf("stored %d, want 1", first.Stored)
	}

	second, err := StoreAttachments("m1", raw, blobs, 0)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Stored != 0 {
		t.Errorf("second call stored %d more, want 0", second.Stored)
	}

	found, _ := ListAttachments("m1")
	if len(found) != 1 {
		t.Errorf("m1 has %d attachments after two calls, want 1", len(found))
	}
}

// A part over the cap is recorded rather than dropped: "there was a 400 MB
// video we did not keep" is information, and silently having no row for it is
// indistinguishable from the message having had no attachment.
func TestStoreAttachmentsRecordsOversizedParts(t *testing.T) {
	blobs := openAttachmentFixture(t)
	raw := rawWithAttachment("m3@acme.com", "huge.pdf", "%PDF-1.4 more than four bytes")

	counts, err := StoreAttachments("m3", raw, blobs, 4)
	if err != nil {
		t.Fatalf("StoreAttachments: %v", err)
	}
	if counts.Stored != 0 || counts.Skipped != 1 {
		t.Fatalf("stored %d skipped %d, want 0/1", counts.Stored, counts.Skipped)
	}

	found, _ := ListAttachments("m3")
	if len(found) != 1 {
		t.Fatalf("m3 has %d attachments, want 1", len(found))
	}
	if found[0].Stored() {
		t.Error("oversized part reports a storage path it does not have")
	}
	if found[0].Skipped == "" {
		t.Error("oversized part records no reason it was skipped")
	}
	if found[0].Size <= 4 {
		t.Errorf("oversized part reports size %d, want its true size", found[0].Size)
	}
}

// Without a blob store there is nowhere to put bytes. Metadata is still worth
// recording — but it must say so, not look like a stored file.
func TestStoreAttachmentsWithoutBlobStore(t *testing.T) {
	openAttachmentFixture(t)
	raw := rawWithAttachment("m1@acme.com", "invoice.pdf", "%PDF-1.4 fake invoice bytes")

	counts, err := StoreAttachments("m1", raw, nil, 0)
	if err != nil {
		t.Fatalf("StoreAttachments: %v", err)
	}
	if counts.Stored != 0 || counts.Skipped != 1 {
		t.Errorf("stored %d skipped %d, want 0/1", counts.Stored, counts.Skipped)
	}

	found, _ := ListAttachments("m1")
	if len(found) != 1 || found[0].Stored() {
		t.Errorf("attachment recorded as stored with no blob store: %+v", found)
	}
}

// The SQL candidate filter and MayHaveAttachments must agree about which mail
// is worth walking. They are built from one marker list precisely so that a
// message either passes both or neither.
func TestCandidateFilterAgreesWithMarkerTest(t *testing.T) {
	openAttachmentFixture(t)

	where, args := unextractedCandidates()
	rows, err := db.Query(`SELECT m.id, m.header FROM messages m WHERE `+where, args...)
	if err != nil {
		t.Fatalf("candidate query: %v", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if !message.MayHaveAttachments(raw) {
			t.Errorf("SQL selected %s as a candidate but MayHaveAttachments rejects it", id)
		}
		seen[id] = true
	}
	if !seen["m1"] || !seen["m2"] {
		t.Errorf("candidates %v, want both m1 and m2", seen)
	}
	if seen["m3"] {
		t.Error("the plain message was selected as an attachment candidate")
	}
}
