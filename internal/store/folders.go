package store

import (
	"fmt"
	"strings"
	"sync"

	"github.com/user/inboxql/internal/message"
)

// Folders the mailbox UI offers.
//
// These are views over one flat table rather than real IMAP folders, because
// that is what InboxQL actually has: sync flattens every folder it fetches into
// `messages`, and the only per-message signals are the IMAP flags, the sender,
// and — since schema v14 — the folder name it was read from.
const (
	FolderAll     = "all"
	FolderInbox   = "inbox"
	FolderStarred = "starred"
	FolderSent    = "sent"
	FolderArchive = "archive"
	FolderDrafts  = "drafts"
	FolderSpam    = "spam"
	FolderTrash   = "trash"
)

// Folders in display order.
var Folders = []string{
	FolderInbox, FolderStarred, FolderSent, FolderArchive,
	FolderDrafts, FolderSpam, FolderTrash,
}

// ValidFolder reports whether a name is one InboxQL knows.
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
// sent from another client that never landed in a folder InboxQL fetched — but it
// would wrongly claim a message you merely appear in the From of, which is why
// the folder check comes first.
// The sender test goes through message_participants rather than from_addr.
// from_addr holds the raw header, so `Me <me@example.com>` never equalled the
// bare account address and mail sent from any client that writes a display
// name — which is most of them — was missing from Sent. The participant row
// carries the normalised address, which is what the account list holds too.
//
// The correlation is written as an unqualified `id` on purpose: this predicate
// is spliced into statements that sometimes alias the table (`FROM messages m`,
// in the query compiler) and sometimes do not (FolderCounts). Unqualified, it
// resolves to the outer row either way; `messages.id` would fail wherever the
// alias is in force.
// sqlIsArchived identifies mail the user filed away.
//
// # Why this had to exist
//
// The inbox was defined as the remainder — not sent, not deleted, not junk —
// which is tidy until a mailbox is mostly archive. On a real one of 43,553
// messages, *zero* were in anything named INBOX and the rail reported
// "Inbox 39,068", every one of them read out of Archive.mbox. The count was
// right and the word was wrong, and there was no way to ask for Archive as
// Archive.
//
// # Why the mailbox name and not a flag
//
// Because there is no \Archived flag; IMAP has no such thing. Archive is a
// place, recorded in `mailbox` since schema v14, and the special-use mailbox is
// named "Archive" by the RFC, by Apple Mail (Archive.mbox) and by every client
// that implements it.
//
// # Why not Gmail's "All Mail"
//
// It looks like the same idea and is not: All Mail contains the inbox too, so
// counting it as archive would empty the inbox of mail that really is in it.
// Gmail archiving is the *absence* of the Inbox label, which this schema does
// not record, so those accounts keep the old behaviour rather than getting a
// wrong answer confidently.
//
// Matched on the trailing segment so a folder merely *containing* the word —
// "Archived invoices", a parent directory — is not swept in.
const sqlIsArchived = `(
	LOWER(COALESCE(mailbox, '')) = 'archive'
	OR LOWER(COALESCE(mailbox, '')) LIKE '%/archive'
	OR LOWER(COALESCE(mailbox, '')) LIKE '%/archive.mbox'
)`

const sqlIsSent = `(
	LOWER(COALESCE(mailbox, '')) LIKE '%sent%'
	OR EXISTS (
		SELECT 1 FROM message_participants p
		WHERE p.message_id = id AND p.role = 'from'
		  AND p.address IN (
			SELECT LOWER(email) FROM accounts WHERE email IS NOT NULL AND email != ''
			UNION
			SELECT LOWER(user)  FROM accounts WHERE user  IS NOT NULL AND user  != ''
		  )
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
	case FolderArchive:
		// Filed away, but not thrown away — and not instead of being sent.
		//
		// The folders have to partition, or the sidebar's counts add up to more
		// than the mailbox. On a real one they did: 4,295 messages were both in
		// Archive and sent by the user, so Archive and Sent each claimed them
		// and the six totals exceeded 43,553.
		//
		// Sent wins, because the two are not the same kind of fact. Sent is
		// about who wrote it and never stops being true; archive is about where
		// it is filed, and filing your own sent mail is what every client does
		// to it eventually. A Sent folder missing everything you have tidied
		// away would be the more surprising of the two.
		return sqlIsArchived + " AND NOT " + sqlIsSent +
			" AND NOT " + sqlIsDeleted + " AND NOT " + sqlIsJunk
	case FolderInbox:
		// Everything that is not filed somewhere else. Defining the inbox as
		// the remainder keeps a message from appearing in two folders — and
		// archive is somewhere else, which it was not before: a mailbox where
		// everything had been filed reported all of it as inbox.
		return "NOT " + sqlIsSent + " AND NOT " + sqlIsDeleted +
			" AND NOT " + sqlIsJunk + " AND NOT " + sqlIsArchived
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
