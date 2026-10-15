package store

import (
	"fmt"
	"strings"
	"sync"

	"github.com/user/inboxql/internal/message"
)

// Folders the mailbox UI offers.
//
// These are views over labels, not places. A message carries a set of labels
// (maillabels.go), and each folder is a question about them: Inbox is "has the
// inbox label and is not binned", All Mail is "not binned", Trash is "has the
// trash label". Unlike the folders they replace they overlap, as Gmail's do:
// your own reply to a thread is in Sent and, if you starred it, in Starred.
const (
	FolderAll       = "all"
	FolderInbox     = "inbox"
	FolderStarred   = "starred"
	FolderSent      = "sent"
	FolderDrafts    = "drafts"
	FolderAllMail   = "allmail"
	FolderArchive   = "archive"
	FolderImportant = "important"
	FolderSpam      = "spam"
	FolderTrash     = "trash"
)

// Folders in display order: the rail's rows.
var Folders = []string{
	FolderInbox, FolderStarred, FolderSent, FolderDrafts,
	FolderAllMail, FolderSpam, FolderTrash,
}

// ValidFolder reports whether a name is one InboxQL knows. Archive and
// Important are valid in a query without being rows of the rail.
func ValidFolder(name string) bool {
	if name == "" || name == FolderAll || name == FolderArchive || name == FolderImportant {
		return true
	}
	for _, f := range Folders {
		if f == name {
			return true
		}
	}
	return false
}

// IsDraftFolder reports whether a folder is served from the drafts table
// rather than from messages.
//
// Drafts are outgoing and unsent; they have never been part of the message
// store and giving them a fake row there would mean a draft could be
// deduplicated against real mail.
func IsDraftFolder(name string) bool { return name == FolderDrafts }

// sqlIsFlagged is starred. Starred is a flag, as it is in IMAP and in Gmail's
// IMAP, not a label.
const sqlIsFlagged = `flags LIKE '%\\Flagged%'`

var (
	sqlInTrash = hasLabelSQL(LabelTrash)
	sqlInSpam  = hasLabelSQL(LabelSpam)
	// Binned mail — Trash and Spam — is left out of every other view, as
	// Gmail leaves it out.
	sqlBinned = "(" + sqlInTrash + " OR " + sqlInSpam + ")"
)

// folderClause returns the SQL predicate for a folder.
func folderClause(folder string) string {
	switch folder {
	case FolderTrash:
		return sqlInTrash
	case FolderSpam:
		return "(" + sqlInSpam + " AND NOT " + sqlInTrash + ")"
	case FolderStarred:
		return "(" + sqlIsFlagged + " AND NOT " + sqlBinned + ")"
	case FolderSent:
		return "(" + hasLabelSQL(LabelSent) + " AND NOT " + sqlBinned + ")"
	case FolderImportant:
		return "(" + hasLabelSQL(LabelImportant) + " AND NOT " + sqlBinned + ")"
	case FolderAllMail:
		return "(NOT " + sqlBinned + ")"
	case FolderArchive:
		// Archived is the absence of Inbox, which is all it ever was in Gmail.
		return "(NOT " + hasLabelSQL(LabelInbox) + " AND NOT " + sqlBinned + ")"
	case FolderInbox:
		return "(" + hasLabelSQL(LabelInbox) + " AND NOT " + sqlBinned + ")"
	default:
		return ""
	}
}

// FolderCount is one row of the sidebar.
type FolderCount struct {
	Folder string `json:"folder"`
	Total  int    `json:"total"`
	Unread int    `json:"unread"`
}

// FolderCounts totals every folder, all at once.
//
// # Why this fans out
//
// It was a loop of QueryRow: six round trips, one connection, on a pool sized
// for eight and a machine with eight cores. SQLite in WAL mode permits
// concurrent readers, and the pool had been sized for a concurrency that never
// happened. Measured on a million-row table, eight independent counts took
// 2.84 s in sequence and 586 ms together.
//
// # Why not one GROUP BY
//
// Because it is slower, which is the opposite of what it looks like. Measured
// on the same table, the per-folder seeks took 0.84 s and a single
// `GROUP BY mailbox` took 1.43 s: each folder's clause is served by an index,
// and grouping instead scans every row including the ones no folder wants.
//
// # Why a slice rather than a channel
//
// Each goroutine owns one index, so there is no shared write and no mutex. The
// result keeps Folders' order without sorting, which matters because the
// sidebar is laid out in that order.
//
// The first error wins and the rest are discarded: they would be six copies of
// the same database failure, and the caller can only report one.
//
// The sidebar needs all of them at once; six round trips from the browser to
// render one list would be silly.
func FolderCounts(accountID string) ([]FolderCount, error) {
	out := make([]FolderCount, len(Folders))
	errs := make([]error, len(Folders))

	var wg sync.WaitGroup
	for i, folder := range Folders {
		wg.Add(1)
		go func(i int, folder string) {
			defer wg.Done()
			out[i], errs[i] = countFolder(folder, accountID)
		}(i, folder)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// countFolder totals one folder.
func countFolder(folder, accountID string) (FolderCount, error) {
	if IsDraftFolder(folder) {
		total, err := countDrafts(accountID)
		if err != nil {
			return FolderCount{}, err
		}
		// A draft is never "unread" — you wrote it.
		return FolderCount{Folder: folder, Total: total}, nil
	}

	var clauses []string
	var args []any
	if c := folderClause(folder); c != "" {
		clauses = append(clauses, c)
	}
	if accountID != "" {
		clauses = append(clauses, "account_id = ?")
		args = append(args, accountID)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	count := FolderCount{Folder: folder}
	err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN flags NOT LIKE '%\\Seen%' THEN 1 ELSE 0 END), 0) FROM messages`+where, args...).
		Scan(&count.Total, &count.Unread)
	if err != nil {
		return FolderCount{}, fmt.Errorf("counting %s: %w", folder, err)
	}
	return count, nil
}

func countDrafts(accountID string) (int, error) {
	query := "SELECT COUNT(*) FROM drafts WHERE status != ?"
	args := []any{DraftStatusSent}
	if accountID != "" {
		query += " AND account_id = ?"
		args = append(args, accountID)
	}
	var n int
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// DraftsAsMessages renders unsent drafts in the shape the message list expects.
//
// The list, the viewer and the API all speak Message; teaching each of them
// about a second type so one folder can exist would be a poor trade. The
// mapped rows carry the draft's id, so a caller can still fetch the real thing.
func DraftsAsMessages(accountID string, limit, offset int) ([]*message.Message, error) {
	drafts, err := ListDrafts("")
	if err != nil {
		return nil, err
	}

	var out []*message.Message
	for _, d := range drafts {
		if d.Status == DraftStatusSent {
			continue
		}
		if accountID != "" && d.AccountID != accountID {
			continue
		}
		out = append(out, &message.Message{
			ID:        d.ID,
			AccountID: d.AccountID,
			Subject:   d.Subject,
			Body:      d.Body,
			To:        d.To,
			Cc:        d.Cc,
			Date:      d.UpdatedAt,
			Mailbox:   FolderDrafts,
			// A draft has been read by definition — it is yours — so it never
			// shows as unread in the list.
			Flags: []string{`\Seen`, `\Draft`},
		})
	}

	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}
