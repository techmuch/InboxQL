package store

import (
	"strings"
	"testing"
)

// The counts are answered from idx_messages_counts and never read a message
// row: the rows hold the bodies, and reading them to count is what made the
// folder counts take seconds on a real mailbox.
func TestCountsReadTheIndexNotTheRows(t *testing.T) {
	openQueryFixture(t)

	plan := func(q string, args ...any) string {
		t.Helper()
		rows, err := db.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var steps []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			steps = append(steps, detail)
		}
		return strings.Join(steps, " | ")
	}

	for _, folder := range []string{FolderInbox, FolderAllMail, FolderArchive, FolderStarred} {
		p := plan(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN flags NOT LIKE '%\\Seen%' THEN 1 ELSE 0 END), 0) FROM messages WHERE ` + folderClause(folder))
		if !strings.Contains(p, "COVERING INDEX idx_messages_counts") {
			t.Errorf("%s count does not read the counts index: %s", folder, p)
		}
	}
	for _, q := range []string{
		"SELECT COUNT(*) FROM messages WHERE account_id = ? AND flags NOT LIKE '%\\Seen%'",
		"SELECT COALESCE(SUM(size), 0) FROM messages WHERE account_id = ?",
		"SELECT MAX(date) FROM messages WHERE account_id = ?",
	} {
		if p := plan(q, "acct"); !strings.Contains(p, "COVERING INDEX idx_messages_counts") {
			t.Errorf("account totals do not read the counts index: %s\n%s", q, p)
		}
	}

	// And the answers are what they were.
	counts, err := FolderCounts("")
	if err != nil {
		t.Fatal(err)
	}
	together := map[string]FolderCount{}
	for _, c := range counts {
		together[c.Folder] = c
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages NOT INDEXED WHERE ` + folderClause(FolderAllMail)).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if together[FolderAllMail].Total != total {
		t.Errorf("all mail = %d through the index, %d from the rows", together[FolderAllMail].Total, total)
	}
}
