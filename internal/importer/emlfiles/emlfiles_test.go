package emlfiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/importer"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rawMessage(subject string) string {
	return "From: a@example.com\r\nTo: b@example.com\r\nSubject: " + subject +
		"\r\nMessage-Id: <" + subject + "@example.com>\r\n\r\nBody.\r\n"
}

func TestDetect(t *testing.T) {
	t.Run("missing folder", func(t *testing.T) {
		d := New(filepath.Join(t.TempDir(), "nope")).Detect()
		if d.Available || d.Readable {
			t.Errorf("a missing folder was reported usable: %+v", d)
		}
	})

	t.Run("empty folder carries advice", func(t *testing.T) {
		d := New(t.TempDir()).Detect()
		if !d.Readable {
			t.Fatal("an empty folder should be readable")
		}
		// The remedy is what tells someone they pointed at the wrong place.
		if !strings.Contains(d.Remedy, "drag") {
			t.Errorf("no advice for an empty folder: %q", d.Remedy)
		}
	})

	t.Run("folder with messages", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "one.eml", rawMessage("One"))
		write(t, dir, "two.eml", rawMessage("Two"))

		d := New(dir).Detect()
		if !d.Readable || !strings.Contains(d.Detail, "2") {
			t.Errorf("detection = %+v, want 2 files", d)
		}
	})
}

func TestFindsFilesRecursivelyAndIgnoresOthers(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.eml", rawMessage("A"))
	write(t, dir, "nested/b.eml", rawMessage("B"))
	write(t, dir, "notes.txt", "not a message")
	write(t, dir, "image.png", "not a message")

	boxes, err := New(dir).Mailboxes(context.Background())
	if err != nil {
		t.Fatalf("Mailboxes: %v", err)
	}
	if len(boxes) != 1 {
		t.Fatalf("got %d mailboxes, want 1", len(boxes))
	}
	if boxes[0].Messages != 2 {
		t.Errorf("found %d messages, want 2 (.txt and .png must be ignored)", boxes[0].Messages)
	}
}

// A message dragged out of Mail.app can arrive as .emlx with its byte-count
// prefix intact. Feeding that prefix to the MIME parser corrupts the first
// header, so it has to be stripped here too.
func TestStripsEMLXPrefix(t *testing.T) {
	dir := t.TempDir()
	rfc := rawMessage("Prefixed")
	write(t, dir, "kept.emlx", fmt.Sprintf("%d\n%s<plist></plist>", len(rfc), rfc))

	iter, err := New(dir).Open(context.Background(), ".", importer.Selection{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer iter.Close()

	if !iter.Next() {
		t.Fatal("no message iterated")
	}
	raw := string(iter.Message().Raw)
	if !strings.HasPrefix(raw, "From: a@example.com") {
		t.Errorf("payload starts %q, want the RFC822 headers", truncateForTest(raw, 40))
	}
	if strings.Contains(raw, "<plist>") {
		t.Error("the trailing plist leaked into the message body")
	}
}

func TestDeepScanCollectsContacts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.eml", rawMessage("A"))
	write(t, dir, "b.eml", "From: c@example.com\r\nTo: d@example.com\r\nSubject: B\r\n\r\nHi.\r\n")

	stats, err := New(dir).Scan(context.Background(), ".", importer.ScanDeep, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// a@, b@, c@, d@ — distinct addresses across both messages.
	if stats.Contacts != 4 {
		t.Errorf("contacts = %d, want 4", stats.Contacts)
	}
	if stats.Messages != 2 {
		t.Errorf("messages = %d, want 2", stats.Messages)
	}
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
