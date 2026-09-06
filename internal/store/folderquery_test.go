package store

import (
	"sort"
	"testing"
)

// Folders are already queries.
//
// folderClause returns a SQL predicate per folder, and folders.go says as much
// in its own doc comment: they are views over one flat table, not real IMAP
// folders. These tests pin the equivalence between each hardcoded predicate and
// the query-language expression for it, which is the safety net for ever
// replacing one with the other.
//
// Where the two disagree, the disagreement is the interesting part.

func queryIDs(t *testing.T, q string) []string {
	t.Helper()
	res, err := RunQuery(q, 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(%q): %v", q, err)
	}
	out := make([]string, 0, len(res.Messages))
	for _, m := range res.Messages {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The folders that map cleanly onto the language.
func TestFoldersAreExpressibleAsQueries(t *testing.T) {
	openFolderFixture(t)

	cases := []struct{ folder, equivalent string }{
		{"trash", "is:deleted"},
		{"spam", "is:junk -is:deleted"},
		{"starred", "is:starred -is:deleted"},
	}

	for _, c := range cases {
		t.Run(c.folder, func(t *testing.T) {
			viaFolder := queryIDs(t, "folder:"+c.folder)
			viaQuery := queryIDs(t, c.equivalent)
			if !sameSet(viaFolder, viaQuery) {
				t.Errorf("folder:%s = %v but %q = %v", c.folder, viaFolder, c.equivalent, viaQuery)
			}
			if len(viaFolder) == 0 {
				t.Errorf("folder:%s matched nothing; the case proves nothing", c.folder)
			}
		})
	}
}

// Sent has to find mail whose From header carries a display name.
//
// sqlIsSent used to compare LOWER(from_addr) — the raw header — against the
// account's addresses, so `Me <me@example.com>` never matched and mail sent
// from any client that writes a display name was missing. It goes through
// message_participants now, which holds the normalised address.
func TestSentFolderFindsDisplayNameSenders(t *testing.T) {
	openFolderFixture(t)

	// The same account address, written the way most clients write it.
	m := newFolderMessage("sent-with-name", "Me <me@example.com>", "", nil)
	if err := SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	viaFolder := queryIDs(t, "folder:sent")
	if !contains(viaFolder, "sent-with-name") {
		t.Errorf("folder:sent missed mail sent with a display name: %v", viaFolder)
	}

	// And it still agrees with the query-language expression for the same idea.
	viaQuery := queryIDs(t, "(mailbox:*sent* OR from:me()) -is:deleted")
	if !sameSet(viaFolder, viaQuery) {
		t.Errorf("folder:sent = %v but the query form = %v", viaFolder, viaQuery)
	}
}

// The inbox is defined as the remainder, so a sender fix has to leave it a
// partition rather than letting a message appear in two folders at once.
func TestFoldersStillPartitionAfterTheSenderFix(t *testing.T) {
	openFolderFixture(t)

	m := newFolderMessage("sent-with-name", "Me <me@example.com>", "", nil)
	if err := SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	if inbox := queryIDs(t, "folder:inbox"); contains(inbox, "sent-with-name") {
		t.Errorf("mail you sent is showing in the inbox: %v", inbox)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
