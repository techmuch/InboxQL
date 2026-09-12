package message

import (
	"encoding/base64"
	"strings"
	"testing"
)

func multipart(parts ...string) []byte {
	var b strings.Builder
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Subject: Test\r\n")
	b.WriteString("From: alice@acme.com\r\n")
	b.WriteString("To: bob@acme.com\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"sep\"\r\n\r\n")
	for _, p := range parts {
		b.WriteString("--sep\r\n")
		b.WriteString(p)
		b.WriteString("\r\n")
	}
	b.WriteString("--sep--\r\n")
	return []byte(b.String())
}

func b64Part(contentType, disposition, payload string) string {
	return "Content-Type: " + contentType + "\r\n" +
		"Content-Disposition: " + disposition + "\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString([]byte(payload))
}

// The text body is not an attachment. ParseRFC822 already has it, and listing
// it here would put "attachment.bin" on every plain message in the mailbox.
func TestExtractAttachmentsSkipsTheBody(t *testing.T) {
	raw := multipart(
		"Content-Type: text/plain; charset=utf-8\r\n\r\nSee attached.",
		b64Part("application/pdf", `attachment; filename="invoice.pdf"`, "%PDF-1.4"),
	)

	parts, err := ExtractAttachments(raw, 0)
	if err != nil {
		t.Fatalf("ExtractAttachments: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("extracted %d parts, want 1", len(parts))
	}
	if parts[0].Filename != "invoice.pdf" {
		t.Errorf("filename %q, want invoice.pdf", parts[0].Filename)
	}
	if string(parts[0].Data) != "%PDF-1.4" {
		t.Errorf("data %q, want the decoded payload", parts[0].Data)
	}
	if parts[0].Size != int64(len("%PDF-1.4")) {
		t.Errorf("size %d, want %d", parts[0].Size, len("%PDF-1.4"))
	}
}

// An inline image is a part worth keeping — it is what a signature logo or an
// embedded screenshot arrives as — and it must carry its Content-ID so the
// body's cid: reference can be resolved later.
func TestExtractAttachmentsKeepsInlineImages(t *testing.T) {
	raw := multipart(
		"Content-Type: text/plain; charset=utf-8\r\n\r\nHello.",
		"Content-Type: image/png\r\n"+
			"Content-Disposition: inline\r\n"+
			"Content-Id: <logo@acme.com>\r\n"+
			"Content-Transfer-Encoding: base64\r\n\r\n"+
			base64.StdEncoding.EncodeToString([]byte("\x89PNG...")),
	)

	parts, err := ExtractAttachments(raw, 0)
	if err != nil {
		t.Fatalf("ExtractAttachments: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("extracted %d parts, want 1", len(parts))
	}
	if !parts[0].Inline {
		t.Error("inline part is not marked inline")
	}
	if parts[0].ContentID != "logo@acme.com" {
		t.Errorf("content id %q, want logo@acme.com without its brackets", parts[0].ContentID)
	}
	// No filename was sent, so one is invented rather than left blank.
	if parts[0].Filename != "image.png" {
		t.Errorf("filename %q, want image.png", parts[0].Filename)
	}
}

// The cap bounds memory, not knowledge: an oversized part is still reported,
// with its real size and a reason, so it is visible rather than absent.
func TestExtractAttachmentsReportsOversizedParts(t *testing.T) {
	payload := strings.Repeat("x", 5000)
	raw := multipart(b64Part("application/pdf", `attachment; filename="big.pdf"`, payload))

	parts, err := ExtractAttachments(raw, 100)
	if err != nil {
		t.Fatalf("ExtractAttachments: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("extracted %d parts, want 1", len(parts))
	}
	if parts[0].Data != nil {
		t.Error("oversized part kept its bytes despite the cap")
	}
	if parts[0].Size != int64(len(payload)) {
		t.Errorf("size %d, want the true size %d", parts[0].Size, len(payload))
	}
	if parts[0].Skipped == "" {
		t.Error("oversized part gives no reason it has no data")
	}
}

// The pre-filter may say "maybe" about mail carrying nothing. It must never say
// "no" about mail that has something, because a false negative means an
// attachment that is never extracted and never reported missing.
func TestMayHaveAttachmentsNeverMissesRealParts(t *testing.T) {
	withParts := [][]byte{
		multipart(b64Part("application/pdf", `attachment; filename="a.pdf"`, "pdf")),
		multipart("Content-Type: image/png\r\nContent-Disposition: inline\r\n\r\nx"),
	}
	for i, raw := range withParts {
		parts, err := ExtractAttachments(raw, 0)
		if err != nil || len(parts) == 0 {
			t.Fatalf("case %d: fixture carries no parts (err %v)", i, err)
		}
		if !MayHaveAttachments(raw) {
			t.Errorf("case %d: pre-filter rejected mail that really has %d part(s)", i, len(parts))
		}
	}

	plain := []byte("Subject: Lunch\r\nContent-Type: text/plain\r\n\r\nFree on Friday?\r\n")
	if MayHaveAttachments(plain) {
		t.Error("pre-filter accepted a plain text message")
	}
}

// Every marker the SQL candidate filter is built from has to be a real string
// that trips the Go test, or the two go out of step silently.
func TestAttachmentMarkersAllTrip(t *testing.T) {
	if len(AttachmentMarkers) == 0 {
		t.Fatal("no markers defined")
	}
	for _, marker := range AttachmentMarkers {
		if !MayHaveAttachments([]byte("Subject: x\r\n" + marker + "\r\n")) {
			t.Errorf("marker %q does not trip MayHaveAttachments", marker)
		}
	}
}

// Malformed mail must not panic or take the message down with it: the body was
// already stored, and a message with unwalkable MIME simply has no attachments.
func TestExtractAttachmentsToleratesGarbage(t *testing.T) {
	for _, raw := range [][]byte{
		nil,
		[]byte(""),
		[]byte("not a message at all"),
		[]byte("Content-Type: multipart/mixed; boundary=\"sep\"\r\n\r\n--sep\r\ntruncated"),
	} {
		parts, err := ExtractAttachments(raw, 0)
		if err == nil && len(parts) > 0 {
			t.Errorf("garbage input yielded %d attachments", len(parts))
		}
	}
}
