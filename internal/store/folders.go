package store

import (
	"fmt"
	"strings"

	"github.com/user/uea/internal/message"
)

// Folders the mailbox UI offers.
//
// These are views over one flat table rather than real IMAP folders, because
// that is what UEA actually has: sync flattens every folder it fetches into
// `messages`, and the only per-message signals are the IMAP flags, the sender,
// and — since schema v14 — the folder name it was read from.
const (
	FolderAll     = "all"
	FolderInbox   = "inbox"
	FolderStarred = "starred"
	FolderSent    = "sent"
	FolderDrafts  = "drafts"
	FolderSpam    = "spam"
	FolderTrash   = "trash"
)

// Folders in display order.
var Folders = []string{FolderInbox, FolderStarred, FolderSent, FolderDrafts, FolderSpam, FolderTrash}

// ValidFolder reports whether a name is one UEA knows.
func ValidFolder(name string) bool {
	if name == "" || name == FolderAll {
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

// Flag predicates. IMAP flags are stored as a JSON array, so these match the
// quoted flag inside it — `"\\Deleted"` in the JSON text.
const (
	sqlIsDeleted = `flags LIKE '%\\Deleted%'`
	sqlIsFlagged = `flags LIKE '%\\Flagged%'`
	sqlIsJunk    = `(flags LIKE '%\\Junk%' OR flags LIKE '%$Junk%')`
)

// sqlIsSent identifies mail the account sent.
//
// Two independent signals, because neither is sufficient alone. The folder it
// was read from is authoritative when present, but is NULL for everything
// synced before v14. The sender address covers those, and also covers mail
// sent from another client that never landed in a folder UEA fetched — but it
// would wrongly claim a message you merely appear in the From of, which is why
// the folder check comes first.
const sqlIsSent = `(
	LOWER(COALESCE(mailbox, '')) LIKE '%sent%'
	OR LOWER(from_addr) IN (
		SELECT LOWER(email) FROM accounts WHERE email IS NOT NULL AND email != ''
		UNION
		SELECT LOWER(user)  FROM accounts WHERE user  IS NOT NULL AND user  != ''
	)
)`

// folderClause returns the SQL predicate for a folder.
func folderClause(folder string) string {
	switch folder {
	case FolderTrash:
		return sqlIsDeleted
	case FolderSpam:
		return sqlIsJunk + " AND NOT " + sqlIsDeleted
	case FolderStarred:
		// Starred is a property, not a place: a flagged message in Sent is
		// still starred. Only deleted mail drops out.
		return sqlIsFlagged + " AND NOT " + sqlIsDeleted
	case FolderSent:
		return sqlIsSent + " AND NOT " + sqlIsDeleted
	case FolderInbox:
		// Everything that is not filed somewhere else. Defining the inbox as
		// the remainder keeps a message from appearing in two folders.
		return "NOT " + sqlIsSent + " AND NOT " + sqlIsDeleted + " AND NOT " + sqlIsJunk
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

// FolderCounts totals every folder in one pass per folder.
//
// The sidebar needs all of them at once; six round trips from the browser to
// render one list would be silly.
func FolderCounts(accountID string) ([]FolderCount, error) {
	out := make([]FolderCount, 0, len(Folders))

	for _, folder := range Folders {
		if IsDraftFolder(folder) {
			total, err := countDrafts(accountID)
			if err != nil {
				return nil, err
			}
			// A draft is never "unread" — you wrote it.
			out = append(out, FolderCount{Folder: folder, Total: total})
			continue
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

		var count FolderCount
		count.Folder = folder
		err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN flags NOT LIKE '%\\Seen%' THEN 1 ELSE 0 END), 0) FROM messages`+where, args...).
			Scan(&count.Total, &count.Unread)
		if err != nil {
			return nil, fmt.Errorf("counting %s: %w", folder, err)
		}
		out = append(out, count)
	}
	return out, nil
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
