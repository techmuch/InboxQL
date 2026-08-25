package applemail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/importer"
)

const store = "/tmp/mailtest/Library/Mail"

func skipWithoutFixture(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(store); err != nil {
		t.Skip("synthetic Apple Mail store not present")
	}
}

func TestDetectFindsHighestVersion(t *testing.T) {
	skipWithoutFixture(t)

	d := NewAt(store).Detect()
	if !d.Available || !d.Readable {
		t.Fatalf("store not detected: %+v", d)
	}
	if filepath.Base(d.Root) != "V10" {
		t.Errorf("root = %s, want the V10 directory", d.Root)
	}
}

func TestDetectDistinguishesAbsentFromBlocked(t *testing.T) {
	absent := NewAt(filepath.Join(t.TempDir(), "nope")).Detect()
	if absent.Available {
		t.Error("a missing store was reported as available")
	}

	// Available-but-unreadable is the TCC case, and it must carry advice rather
	// than a bare errno.
	blocked := t.TempDir()
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Skip("cannot create an unreadable directory here")
	}
	t.Cleanup(func() { os.Chmod(blocked, 0o755) })

	if os.Geteuid() == 0 {
		t.Skip("running as root; permission cannot be denied")
	}
	d := NewAt(blocked).Detect()
	if !d.Available || d.Readable {
		t.Fatalf("expected available-but-unreadable, got %+v", d)
	}
	if !strings.Contains(d.Remedy, "Full Disk Access") {
		t.Errorf("remedy does not mention Full Disk Access: %q", d.Remedy)
	}
}

func TestMailboxesIncludeNestedFolders(t *testing.T) {
	skipWithoutFixture(t)

	boxes, err := NewAt(store).Mailboxes(context.Background())
	if err != nil {
		t.Fatalf("Mailboxes: %v", err)
	}

	paths := map[string]importer.Mailbox{}
	for _, b := range boxes {
		paths[b.Path] = b
	}
	for _, want := range []string{"INBOX", "Sent Messages", "Archive", "Archive/2024"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("mailbox %q missing; got %v", want, keys(paths))
		}
	}

	// A parent must not absorb its child's messages, or every count up the
	// tree is inflated.
	if got := paths["Archive"].Messages; got != 0 {
		t.Errorf("Archive holds %d messages of its own, want 0", got)
	}
	if got := paths["Archive/2024"].Messages; got != 3 {
		t.Errorf("Archive/2024 = %d messages, want 3", got)
	}
	if paths["Archive/2024"].ParentID != paths["Archive"].ID {
		t.Error("nested mailbox is not linked to its parent")
	}
	// INBOX holds 10 .emlx files. Messages counts what is present; the one
	// partial download is reported separately rather than deducted, so a scan
	// says "10 messages, 1 never downloaded" rather than quietly showing 9.
	if got := paths["INBOX"].Messages; got != 10 {
		t.Errorf("INBOX = %d messages, want 10", got)
	}
}

func TestDeepScanCountsWhatFastScanCannot(t *testing.T) {
	skipWithoutFixture(t)
	src := NewAt(store)
	inbox := mailboxID(t, src, "INBOX")

	fast, err := src.Scan(context.Background(), inbox, importer.ScanFast, nil)
	if err != nil {
		t.Fatalf("fast scan: %v", err)
	}
	if fast.Messages != 10 {
		t.Errorf("fast scan messages = %d, want 10", fast.Messages)
	}
	if fast.Partial != 1 {
		t.Errorf("fast scan partial = %d, want 1", fast.Partial)
	}
	// Fast scan must not invent numbers it did not compute.
	if fast.Attachments != 0 || fast.Contacts != 0 {
		t.Error("fast scan reported deep-only statistics")
	}

	deep, err := src.Scan(context.Background(), inbox, importer.ScanDeep, nil)
	if err != nil {
		t.Fatalf("deep scan: %v", err)
	}
	if deep.Attachments < 1 {
		t.Errorf("deep scan found %d attachments, want at least 1", deep.Attachments)
	}
	if deep.Contacts < 2 {
		t.Errorf("deep scan found %d contacts, want several", deep.Contacts)
	}
	if deep.Oldest.IsZero() || deep.Newest.IsZero() {
		t.Error("deep scan did not establish a date range")
	}
	if deep.Unread < 1 {
		t.Errorf("deep scan unread = %d, want at least 1", deep.Unread)
	}
}

func TestOpenSkipsPartialsAndReadsFlags(t *testing.T) {
	skipWithoutFixture(t)
	src := NewAt(store)

	iter, err := src.Open(context.Background(), mailboxID(t, src, "INBOX"), importer.Selection{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer iter.Close()

	var partials, seen, total int
	for iter.Next() {
		m := iter.Message()
		total++
		if m.Partial {
			partials++
			continue
		}
		if len(m.Raw) == 0 {
			t.Errorf("%s yielded no bytes", m.SourceID)
		}
		for _, f := range m.Flags {
			if f == `\Seen` {
				seen++
			}
		}
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iteration: %v", err)
	}
	if total != 10 {
		t.Errorf("iterated %d messages, want 10", total)
	}
	if partials != 1 {
		t.Errorf("found %d partial messages, want 1", partials)
	}
	if seen == 0 {
		t.Error("no \\Seen flags were decoded from any .emlx plist")
	}
}

func mailboxID(t *testing.T, src *Source, path string) string {
	t.Helper()
	boxes, err := src.Mailboxes(context.Background())
	if err != nil {
		t.Fatalf("Mailboxes: %v", err)
	}
	for _, b := range boxes {
		if b.Path == path {
			return b.ID
		}
	}
	t.Fatalf("no mailbox at %q", path)
	return ""
}

func keys(m map[string]importer.Mailbox) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
