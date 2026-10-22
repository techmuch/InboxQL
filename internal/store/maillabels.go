package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/user/inboxql/internal/message"
)

// # Labels, not folders
//
// A message carries a set of labels, as Gmail's messages do. The mailbox is
// not a tree of folders that each hold a message once: Inbox, Sent, Spam and
// Trash are labels like any other, and archiving a message is taking Inbox
// off it.
//
// The folder views the Desk and `folder:` offer are defined over labels (see
// folderClause), so they are no longer a partition. A message can be in Sent
// and under Receipts at once, which is what was true of it all along.
//
// These are mail labels: put on by a person, by the server or by the import.
// Annotator labels are the other kind, and `label:` answers for both.

// System labels, as stored.
const (
	LabelInbox     = "inbox"
	LabelSent      = "sent"
	LabelDrafts    = "drafts"
	LabelImportant = "important"
	LabelSpam      = "spam"
	LabelTrash     = "trash"
)

// SystemLabels in display order.
var SystemLabels = []string{LabelInbox, LabelSent, LabelDrafts, LabelImportant, LabelSpam, LabelTrash}

// Where a label came from.
const (
	LabelSourceServer    = "server"
	LabelSourceImport    = "import"
	LabelSourceHuman     = "human"
	LabelSourceAgent     = "agent"
	LabelSourceMigration = "migration"
)

// IsSystemLabel reports whether a name is one of the system labels.
func IsSystemLabel(name string) bool {
	n := strings.ToLower(name)
	for _, l := range SystemLabels {
		if l == n {
			return true
		}
	}
	return false
}

// gmailSystem maps Gmail's X-GM-LABELS atoms to system labels. \Starred is
// the \Flagged flag here, as it is in IMAP, so it maps to nothing.
var gmailSystem = map[string]string{
	`\inbox`:     LabelInbox,
	`\sent`:      LabelSent,
	`\draft`:     LabelDrafts,
	`\important`: LabelImportant,
	`\spam`:      LabelSpam,
	`\trash`:     LabelTrash,
	`\starred`:   "",
}

// NormalizeLabel returns the stored form of a label name: a system label in
// lower case, Gmail's \Inbox and friends as the system label they mean, and
// anything else trimmed. Empty means "not a label".
func NormalizeLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, `\`) {
		if l, ok := gmailSystem[strings.ToLower(name)]; ok {
			return l
		}
		return ""
	}
	if IsSystemLabel(name) {
		return strings.ToLower(name)
	}
	return name
}

// GmailLabelName is a label as X-GM-LABELS spells it, for writing back.
func GmailLabelName(label string) string {
	switch label {
	case LabelInbox:
		return `\Inbox`
	case LabelSent:
		return `\Sent`
	case LabelDrafts:
		return `\Draft`
	case LabelImportant:
		return `\Important`
	}
	return label
}

var uuidSegment = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// LabelForMailbox is the label a folder name stands for.
//
// It reads the names IMAP servers, Gmail and Apple Mail's export give folders:
// `INBOX`, `[Gmail]/Sent Mail`, `<uuid>/Archive.mbox`, `Work.mbox/Clients.mbox`.
// specialUse is the server's RFC 6154 attribute when there is one, which wins
// over the name. Archive and All Mail have no label: being in neither Inbox,
// Spam nor Trash is what archived means. The empty result with ok true says
// so; ok false means the name was empty.
func LabelForMailbox(path, specialUse string) (label string, ok bool) {
	switch strings.ToLower(specialUse) {
	case `\sent`:
		return LabelSent, true
	case `\drafts`:
		return LabelDrafts, true
	case `\junk`:
		return LabelSpam, true
	case `\trash`:
		return LabelTrash, true
	case `\important`:
		return LabelImportant, true
	case `\all`, `\archive`, `\flagged`:
		return "", true
	}

	var segs []string
	for _, s := range strings.Split(strings.ReplaceAll(path, `\`, "/"), "/") {
		s = strings.TrimSuffix(strings.TrimSpace(s), ".mbox")
		if s == "" || uuidSegment.MatchString(s) {
			continue
		}
		segs = append(segs, s)
	}
	if len(segs) == 0 {
		return "", false
	}
	gmail := strings.EqualFold(segs[0], "[Gmail]") || strings.EqualFold(segs[0], "[Google Mail]")
	if gmail && len(segs) == 1 {
		return "", true
	}
	last := strings.ToLower(segs[len(segs)-1])
	switch last {
	case "inbox":
		if len(segs) == 1 {
			return LabelInbox, true
		}
	case "sent", "sent mail", "sent messages", "sent items", "sent-mail":
		return LabelSent, true
	case "drafts", "draft":
		return LabelDrafts, true
	case "spam", "junk", "junk email", "junk e-mail", "bulk mail":
		return LabelSpam, true
	case "trash", "bin", "deleted", "deleted messages", "deleted items":
		return LabelTrash, true
	case "archive", "archives", "all mail":
		return "", true
	case "important":
		if gmail {
			return LabelImportant, true
		}
	case "starred", "flagged":
		if gmail {
			return "", true
		}
	}
	if gmail {
		segs = segs[1:]
	}
	return strings.Join(segs, "/"), true
}

// ensureLabels records that labels exist. System labels always do.
func ensureLabels(x execer, names ...string) error {
	now := time.Now().UnixMilli()
	for _, n := range names {
		if n == "" || IsSystemLabel(n) {
			continue
		}
		if _, err := x.Exec(`INSERT OR IGNORE INTO mail_labels (name, system, created_at) VALUES (?, 0, ?)`, n, now); err != nil {
			return err
		}
	}
	return nil
}

// seedSystemLabels writes the system labels' rows.
func seedSystemLabels(x execer) error {
	now := time.Now().UnixMilli()
	for _, n := range SystemLabels {
		if _, err := x.Exec(`INSERT OR IGNORE INTO mail_labels (name, system, created_at) VALUES (?, 1, ?)`, n, now); err != nil {
			return err
		}
	}
	return nil
}

// backfillLabels gives every stored message the labels its folder and flags
// imply. Run once, by the v43 migration; it only adds, so running it again
// changes nothing.
func backfillLabels(db *sql.DB) error {
	if err := seedSystemLabels(db); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT DISTINCT COALESCE(mailbox, '') FROM messages`)
	if err != nil {
		return err
	}
	var boxes []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		boxes = append(boxes, b)
	}
	rows.Close()

	now := time.Now().UnixMilli()
	for _, box := range boxes {
		label, ok := LabelForMailbox(box, "")
		if !ok || label == "" {
			continue
		}
		if err := ensureLabels(db, label); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at)
			SELECT id, ?, 'migration', ? FROM messages WHERE COALESCE(mailbox, '') = ?`, label, now, box); err != nil {
			return err
		}
	}

	// What the flags and the sender say, whatever the folder.
	for _, s := range []struct{ label, where string }{
		{LabelTrash, `flags LIKE '%\\Deleted%'`},
		{LabelSpam, `(flags LIKE '%\\Junk%' OR flags LIKE '%$Junk%')`},
		{LabelSent, sqlFromMe},
		// Synced before folders were recorded: it came from INBOX or a Sent
		// folder, and the sender says which.
		{LabelInbox, `COALESCE(mailbox, '') = '' AND NOT ` + sqlFromMe},
	} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at)
			SELECT id, ?, 'migration', ? FROM messages WHERE `+s.where, s.label, now); err != nil {
			return fmt.Errorf("labelling %s: %w", s.label, err)
		}
	}
	return nil
}

// sqlFromMe is true of a message one of the accounts sent.
const sqlFromMe = `EXISTS (
	SELECT 1 FROM message_participants p
	WHERE p.message_id = id AND p.role = 'from'
	  AND p.address IN (
		SELECT LOWER(email) FROM accounts WHERE email IS NOT NULL AND email != ''
		UNION
		SELECT LOWER(user)  FROM accounts WHERE user  IS NOT NULL AND user  != ''
	  )
)`

// hasLabelSQL is true of a message carrying a label. The correlation is an
// unqualified id, for the reason sqlIsSent gives.
func hasLabelSQL(label string) string { return hasLabelOver("", label) }

// hasLabelOver is hasLabelSQL correlated to a table alias, for statements
// with more than one id in scope.
func hasLabelOver(alias, label string) string {
	id := "id"
	if alias != "" {
		id = alias + ".id"
	}
	return `EXISTS (SELECT 1 FROM message_labels ml WHERE ml.message_id = ` + id + ` AND ml.label = '` +
		strings.ReplaceAll(label, "'", "''") + `')`
}

// labelsOnSave writes a new message's labels: the ones it was given, or the
// ones its folder, flags and sender imply — the same reading the v43
// migration gave the mail already stored.
func labelsOnSave(x execer, id string, labels []string, mailbox string, source string) error {
	now := time.Now().UnixMilli()
	derive := labels == nil
	noFolder := false
	if derive {
		if l, ok := LabelForMailbox(mailbox, ""); ok && l != "" {
			labels = []string{l}
		} else if !ok {
			noFolder = true
		}
	}
	if source == "" {
		source = LabelSourceImport
	}
	if err := ensureLabels(x, labels...); err != nil {
		return err
	}
	for _, l := range labels {
		if l = NormalizeLabel(l); l == "" {
			continue
		}
		if _, err := x.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at) VALUES (?, ?, ?, ?)`,
			id, l, source, now); err != nil {
			return err
		}
	}
	if !derive {
		return nil
	}
	for _, s := range []struct{ label, where string }{
		{LabelTrash, `flags LIKE '%\\Deleted%'`},
		{LabelSpam, `(flags LIKE '%\\Junk%' OR flags LIKE '%$Junk%')`},
		// Sent is also who wrote it: mail you sent from another client lands
		// wherever that client filed it.
		{LabelSent, sqlFromMe},
	} {
		if _, err := x.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at)
			SELECT id, ?, ?, ? FROM messages WHERE id = ? AND `+s.where, s.label, source, now, id); err != nil {
			return err
		}
	}
	if noFolder {
		// No folder recorded: it arrived, unless it was yours.
		if _, err := x.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at)
			SELECT id, 'inbox', ?, ? FROM messages WHERE id = ? AND NOT `+sqlFromMe, source, now, id); err != nil {
			return err
		}
	}
	return nil
}

// MailLabels returns each message's labels, sorted, system labels first.
func MailLabels(ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		chunk := ids[start:end]
		rows, err := db.Query(`SELECT message_id, label FROM message_labels WHERE message_id IN (`+
			placeholders(len(chunk))+`)`, anySlice(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, l string
			if err := rows.Scan(&id, &l); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = append(out[id], l)
		}
		rows.Close()
	}
	for id := range out {
		sortLabels(out[id])
	}
	return out, nil
}

// attachLabels fills in each message's labels, for showing. A message with
// none gets an empty list rather than nil, so a reader can tell "archived"
// from "not looked up".
func attachLabels(msgs []*message.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	labels, err := MailLabels(ids)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		m.Labels = labels[m.ID]
		if m.Labels == nil {
			m.Labels = []string{}
		}
	}
	return nil
}

func sortLabels(ls []string) {
	rank := func(l string) int {
		for i, s := range SystemLabels {
			if s == l {
				return i
			}
		}
		return len(SystemLabels)
	}
	sort.SliceStable(ls, func(i, j int) bool {
		ri, rj := rank(ls[i]), rank(ls[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(ls[i]) < strings.ToLower(ls[j])
	})
}

// LabelExists reports whether a mail label exists, case-insensitively.
func LabelExists(name string) bool {
	if IsSystemLabel(name) {
		return true
	}
	if db == nil {
		return false
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM mail_labels WHERE name = ?`, name).Scan(&n)
	return n > 0
}

// LabelCount is a label with how many messages carry it.
type LabelCount struct {
	Name   string `json:"name"`
	System bool   `json:"system"`
	Total  int    `json:"total"`
	Unread int    `json:"unread"`
}

// LabelCounts lists every mail label with its totals. Trash and Spam are not
// counted under other labels, as Gmail does not show them there.
func LabelCounts(accountID string) ([]LabelCount, error) {
	args := []any{}
	acct := ""
	if accountID != "" {
		acct = " AND m.account_id = ?"
		args = append(args, accountID)
	}
	rows, err := db.Query(`
		SELECT l.name, l.system,
		       COUNT(m.id),
		       COALESCE(SUM(CASE WHEN m.id IS NOT NULL AND m.flags NOT LIKE '%\\Seen%' THEN 1 ELSE 0 END), 0)
		FROM mail_labels l
		LEFT JOIN message_labels ml ON ml.label = l.name
		LEFT JOIN messages m ON m.id = ml.message_id`+acct+`
		  AND (l.name IN ('trash', 'spam') OR NOT EXISTS (
		        SELECT 1 FROM message_labels b WHERE b.message_id = m.id AND b.label IN ('trash', 'spam')))
		GROUP BY l.name, l.system`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LabelCount
	for rows.Next() {
		var c LabelCount
		if err := rows.Scan(&c.Name, &c.System, &c.Total, &c.Unread); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	names := make([]string, len(out))
	for i, c := range out {
		names[i] = c.Name
	}
	sortLabels(names)
	pos := map[string]int{}
	for i, n := range names {
		pos[n] = i
	}
	sort.Slice(out, func(i, j int) bool { return pos[out[i].Name] < pos[out[j].Name] })
	return out, rows.Err()
}

// LabelChange is what one call to ChangeLabels does.
type LabelChange struct {
	Add    []string
	Remove []string
	// Source is who did it: human or agent.
	Source string
}

// ChangeLabels adds and removes labels on messages, and queues the change for
// each message's server. Returns how many messages changed.
func ChangeLabels(ids []string, ch LabelChange) (int, error) {
	deltas, err := changeLabels(ids, ch)
	return len(deltas), err
}

// labelDelta is what one change did to one message: exactly the labels added
// and removed, which is what undoing it has to reverse — no more.
type labelDelta struct {
	added, removed []string
}

func changeLabels(ids []string, ch LabelChange) (map[string]*labelDelta, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var add, remove []string
	for _, l := range ch.Add {
		if n := NormalizeLabel(l); n != "" {
			add = append(add, n)
		}
	}
	for _, l := range ch.Remove {
		if n := NormalizeLabel(l); n != "" {
			remove = append(remove, n)
		}
	}
	if len(add) == 0 && len(remove) == 0 {
		return nil, fmt.Errorf("no label to add or remove")
	}
	if ch.Source == "" {
		ch.Source = LabelSourceHuman
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := ensureLabels(tx, add...); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	changed := map[string]*labelDelta{}
	accounts := map[string]bool{}
	for _, id := range ids {
		var acct string
		if err := tx.QueryRow(`SELECT account_id FROM messages WHERE id = ?`, id).Scan(&acct); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		for _, l := range add {
			res, err := tx.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at) VALUES (?, ?, ?, ?)`,
				id, l, ch.Source, now)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				deltaFor(changed, id).added = append(deltaFor(changed, id).added, l)
				if err := enqueue(tx, acct, id, OpLabelAdd, l, now); err != nil {
					return nil, err
				}
				accounts[acct] = true
			}
		}
		for _, l := range remove {
			res, err := tx.Exec(`DELETE FROM message_labels WHERE message_id = ? AND label = ?`, id, l)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				deltaFor(changed, id).removed = append(deltaFor(changed, id).removed, l)
				if err := enqueue(tx, acct, id, OpLabelRemove, l, now); err != nil {
					return nil, err
				}
				accounts[acct] = true
			}
		}
	}
	if ch.Source == LabelSourceHuman {
		for id, d := range changed {
			if err := noteUndone(tx, id, d.removed, d.added, now); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	notifyPending(accounts)
	return changed, nil
}

func deltaFor(m map[string]*labelDelta, id string) *labelDelta {
	if m[id] == nil {
		m[id] = &labelDelta{}
	}
	return m[id]
}

// The moves a mail client offers, each a change of labels.
var (
	MoveArchive = LabelChange{Remove: []string{LabelInbox}}
	MoveInbox   = LabelChange{Add: []string{LabelInbox}, Remove: []string{LabelTrash, LabelSpam}}
	MoveTrash   = LabelChange{Add: []string{LabelTrash}, Remove: []string{LabelInbox, LabelSpam}}
	MoveSpam    = LabelChange{Add: []string{LabelSpam}, Remove: []string{LabelInbox, LabelTrash}}
	// Restoring from Trash or Spam puts the message back in the inbox, as
	// Gmail's "Move to Inbox" does.
	MoveUntrash = LabelChange{Add: []string{LabelInbox}, Remove: []string{LabelTrash}}
	MoveNotSpam = LabelChange{Add: []string{LabelInbox}, Remove: []string{LabelSpam}}
)

// Moves by name, for the API and the CLI.
var Moves = map[string]LabelChange{
	"archive": MoveArchive,
	"inbox":   MoveInbox,
	"trash":   MoveTrash,
	"spam":    MoveSpam,
	"untrash": MoveUntrash,
	"notspam": MoveNotSpam,
}

// DeleteLabel removes a user label from every message and forgets it. A
// system label cannot be deleted.
func DeleteLabel(name string) (int, error) {
	if IsSystemLabel(name) {
		return 0, fmt.Errorf("%s is a system label and cannot be deleted", name)
	}
	ids, err := idsWithLabel(name)
	if err != nil {
		return 0, err
	}
	n := 0
	if len(ids) > 0 {
		if n, err = ChangeLabels(ids, LabelChange{Remove: []string{name}}); err != nil {
			return 0, err
		}
	}
	if _, err = db.Exec(`DELETE FROM mail_labels WHERE name = ? AND system = 0`, name); err != nil {
		return n, err
	}
	// On Gmail the label is a thing of its own, which taking it off every
	// message leaves behind, empty. Each Gmail account deletes it there.
	accs, err := ListAccounts()
	if err != nil {
		return n, err
	}
	for _, a := range accs {
		if a.Host != "" && !a.Disconnected && AccountIsGmail(a.ID) {
			if err := queueLabelDelete(a.ID, name); err != nil {
				return n, err
			}
			notifyPending(map[string]bool{a.ID: true})
		}
	}
	return n, nil
}

func labelDeletesKey(accountID string) string { return "mirror.deleteLabels." + accountID }

func queueLabelDelete(accountID, name string) error {
	names := LabelDeletes(accountID)
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return nil
		}
	}
	b, _ := json.Marshal(append(names, name))
	return UpdateSetting(labelDeletesKey(accountID), string(b))
}

// LabelDeletes are labels deleted here that an account's server still has.
func LabelDeletes(accountID string) []string {
	raw, _ := GetSetting(labelDeletesKey(accountID))
	var names []string
	json.Unmarshal([]byte(raw), &names)
	return names
}

// LabelDeleted records that the server has deleted a label too.
func LabelDeleted(accountID, name string) error {
	var keep []string
	for _, n := range LabelDeletes(accountID) {
		if !strings.EqualFold(n, name) {
			keep = append(keep, n)
		}
	}
	b, _ := json.Marshal(keep)
	return UpdateSetting(labelDeletesKey(accountID), string(b))
}

// CreateLabel records a label before any message carries it.
func CreateLabel(name string) error {
	name = NormalizeLabel(name)
	if name == "" {
		return fmt.Errorf("a label needs a name")
	}
	if strings.ContainsAny(name, "\r\n\"") {
		return fmt.Errorf("a label name cannot contain quotes or line breaks")
	}
	return ensureLabels(db, name)
}

func idsWithLabel(name string) ([]string, error) {
	rows, err := db.Query(`SELECT message_id FROM message_labels WHERE label = ?`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
