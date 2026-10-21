package store

import (
	"strings"
	"testing"
)

// A bulk action reaches every match, says what it did, and its undo reverses
// exactly that — not "put everything in the inbox".
func TestBulkActionsAndTheirUndo(t *testing.T) {
	openQueryFixture(t)
	// m4 was archived already; archiving all mail must not let undo put it
	// back in the inbox.
	if _, err := ChangeLabels([]string{"m4"}, MoveArchive); err != nil {
		t.Fatal(err)
	}
	before := strings.Join(ids(t, "folder:inbox"), ",")

	n, err := QueryMessageCount("folder:allmail | timeline")
	if err != nil || n != 5 {
		t.Fatalf("count = %d, %v", n, err)
	}
	out, err := BulkApply("folder:allmail | timeline", "archive", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Matched != 5 || out.Changed != 4 {
		t.Errorf("archive: %+v, want 5 matched and 4 changed", out)
	}
	if got := ids(t, "folder:inbox"); len(got) != 0 {
		t.Errorf("inbox after archiving all = %v", got)
	}
	if _, err := BulkUndoApply(out.Undo); err != nil {
		t.Fatal(err)
	}
	if after := strings.Join(ids(t, "folder:inbox"), ","); after != before {
		t.Errorf("after undo the inbox is %q, want %q", after, before)
	}

	// Flags, and their undo, on just the ones that changed.
	read, err := BulkApply("from:*@acme.com", "read", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(t, "from:*@acme.com is:unread"); len(got) != 0 {
		t.Errorf("still unread: %v", got)
	}
	if _, err := BulkUndoApply(read.Undo); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, "is:unread"); strings.Join(got, ",") != "m1,m3,m5" {
		t.Errorf("unread after undo = %v", got)
	}

	// Labels.
	if out, err := BulkApply("from:billing@stripe.com", "label", "Receipts"); err != nil || out.Changed != 1 {
		t.Fatalf("label: %+v %v", out, err)
	}
	if got := ids(t, "label:receipts"); strings.Join(got, ",") != "m3" {
		t.Errorf("label:receipts = %v", got)
	}

	for _, q := range []string{"in:tickets", "folder:inbox | count by from"} {
		if _, err := BulkApply(q, "archive", ""); err == nil {
			t.Errorf("a bulk action on %q was accepted", q)
		}
	}
	if _, err := BulkApply("folder:inbox", "explode", ""); err == nil {
		t.Error("an unknown action was accepted")
	}
}
