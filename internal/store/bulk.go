package store

import (
	"fmt"
	"sort"
	"strings"

	"github.com/user/inboxql/internal/query"
)

// Acting on every message a query matches, not just the ones on screen.
//
// Selecting a page and archiving it fifty at a time is how a mail client
// makes "archive all the newsletters" an afternoon. A bulk action names the
// query instead, resolves it here, and says how many it touched — and what to
// do to undo exactly that, because a change to thousands of messages has to
// be reversible to be safe to offer.

// BulkMax caps one bulk action. Past it the action covers the newest BulkMax
// and says so; doing it again covers the rest.
const BulkMax = 50000

// BulkActions are the actions a bulk change can make.
var BulkActions = []string{"archive", "inbox", "trash", "spam", "untrash", "notspam",
	"read", "unread", "star", "unstar", "label", "unlabel"}

// BulkResult is what a bulk action did.
type BulkResult struct {
	Matched int `json:"matched"`
	Changed int `json:"changed"`
	// Capped says more matched than one action covers.
	Capped bool     `json:"capped,omitempty"`
	Undo   BulkUndo `json:"undo"`
}

// BulkUndo reverses one bulk action: per group of messages, the labels to
// put back and take off, or the flag to set back.
type BulkUndo struct {
	Labels []BulkLabelOp `json:"labels,omitempty"`
	Flag   *BulkFlagOp   `json:"flag,omitempty"`
}

// BulkLabelOp adds and removes labels on a set of messages.
type BulkLabelOp struct {
	IDs    []string `json:"ids"`
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// BulkFlagOp sets a flag on or off on a set of messages.
type BulkFlagOp struct {
	IDs  []string `json:"ids"`
	Flag string   `json:"flag"`
	On   bool     `json:"on"`
}

// displayStages are the stages that change how mail is shown, not which mail
// it is: a bulk action on `folder:inbox | timeline` acts on the inbox's mail.
var displayStages = map[query.StageKind]bool{query.StageTimeline: true, query.StageThread: true,
	query.StageSort: true}

// QueryMessageIDs resolves a query about mail to message ids, newest first,
// at most max of them; capped says there were more.
func QueryMessageIDs(src string, max int) (ids []string, capped bool, err error) {
	where, args, err := bulkWhere(src)
	if err != nil {
		return nil, false, err
	}
	rows, err := db.Query("SELECT m.id FROM messages m WHERE "+where+" ORDER BY m.date DESC LIMIT ?",
		append(args, max+1)...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		ids = append(ids, id)
	}
	if len(ids) > max {
		ids, capped = ids[:max], true
	}
	return ids, capped, rows.Err()
}

// BulkApply does an action to every message a query matches, as a person.
// label is the label for label and unlabel.
func BulkApply(src, action, label string) (*BulkResult, error) {
	return BulkApplyAs(src, action, label, LabelSourceHuman)
}

// BulkApplyAs is BulkApply with who did it named: human, or agent for a
// harness — recorded on the labels it adds, as a single change is.
func BulkApplyAs(src, action, label, source string) (*BulkResult, error) {
	ids, capped, err := QueryMessageIDs(src, BulkMax)
	if err != nil {
		return nil, err
	}
	out := &BulkResult{Matched: len(ids), Capped: capped}
	if len(ids) == 0 {
		return out, nil
	}

	flag := func(name string, on bool) error {
		changed, err := chunked(ids, func(part []string) ([]string, error) { return setMessageFlag(part, name, on) })
		if err != nil {
			return err
		}
		out.Changed = len(changed)
		if len(changed) > 0 {
			out.Undo.Flag = &BulkFlagOp{IDs: changed, Flag: name, On: !on}
		}
		return nil
	}
	labels := func(ch LabelChange) error {
		ch.Source = source
		deltas := map[string]*labelDelta{}
		for start := 0; start < len(ids); start += 1000 {
			part, err := changeLabels(ids[start:min(start+1000, len(ids))], ch)
			if err != nil {
				return err
			}
			for id, d := range part {
				deltas[id] = d
			}
		}
		out.Changed = len(deltas)
		out.Undo.Labels = undoLabels(deltas)
		return nil
	}

	switch action {
	case "read":
		err = flag(FlagSeen, true)
	case "unread":
		err = flag(FlagSeen, false)
	case "star":
		err = flag(FlagFlagged, true)
	case "unstar":
		err = flag(FlagFlagged, false)
	case "label", "unlabel":
		if strings.TrimSpace(label) == "" {
			return nil, fmt.Errorf("which label?")
		}
		ch := LabelChange{Add: []string{label}}
		if action == "unlabel" {
			ch = LabelChange{Remove: []string{label}}
		}
		err = labels(ch)
	default:
		move, ok := Moves[action]
		if !ok {
			return nil, fmt.Errorf("%q is not a bulk action (%s)", action, strings.Join(BulkActions, ", "))
		}
		err = labels(move)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// chunked runs a change over ids a thousand at a time, the size the single
// message endpoints already accept in one transaction.
func chunked(ids []string, f func([]string) ([]string, error)) ([]string, error) {
	var all []string
	for start := 0; start < len(ids); start += 1000 {
		part, err := f(ids[start:min(start+1000, len(ids))])
		if err != nil {
			return all, err
		}
		all = append(all, part...)
	}
	return all, nil
}

// undoLabels groups the reverse of each message's change, so undoing
// archiving a thousand messages is one operation and not a thousand.
func undoLabels(deltas map[string]*labelDelta) []BulkLabelOp {
	groups := map[string]*BulkLabelOp{}
	var keys []string
	for id, d := range deltas {
		add := append([]string{}, d.removed...)
		remove := append([]string{}, d.added...)
		sort.Strings(add)
		sort.Strings(remove)
		key := strings.Join(add, ",") + "|" + strings.Join(remove, ",")
		g := groups[key]
		if g == nil {
			g = &BulkLabelOp{Add: add, Remove: remove}
			groups[key] = g
			keys = append(keys, key)
		}
		g.IDs = append(g.IDs, id)
	}
	sort.Strings(keys)
	out := make([]BulkLabelOp, 0, len(keys))
	for _, k := range keys {
		sort.Strings(groups[k].IDs)
		out = append(out, *groups[k])
	}
	return out
}

// BulkUndoApply reverses a bulk action, returning how many messages changed
// back.
func BulkUndoApply(u BulkUndo) (int, error) {
	n := 0
	for _, op := range u.Labels {
		changed, err := chunked(op.IDs, func(part []string) ([]string, error) {
			d, err := changeLabels(part, LabelChange{Add: op.Add, Remove: op.Remove})
			out := make([]string, 0, len(d))
			for id := range d {
				out = append(out, id)
			}
			return out, err
		})
		if err != nil {
			return n, err
		}
		n += len(changed)
	}
	if u.Flag != nil {
		changed, err := chunked(u.Flag.IDs, func(part []string) ([]string, error) {
			return setMessageFlag(part, u.Flag.Flag, u.Flag.On)
		})
		if err != nil {
			return n, err
		}
		n += len(changed)
	}
	return n, nil
}

// QueryMessageCount is how many messages a bulk action on the query would
// reach, for "Select all 1,234".
func QueryMessageCount(src string) (int, error) {
	where, args, err := bulkWhere(src)
	if err != nil {
		return 0, err
	}
	var n int
	err = db.QueryRow("SELECT COUNT(*) FROM messages m WHERE "+where, args...).Scan(&n)
	return n, err
}

// bulkWhere compiles a query about mail to the WHERE clause its messages
// answer, refusing one that turns mail into something else.
func bulkWhere(src string) (string, []any, error) {
	q, err := query.Parse(src)
	if err != nil {
		return "", nil, err
	}
	if q.Entity() != query.EntityMessage {
		return "", nil, fmt.Errorf("a bulk action is about mail, and this query is about %s", q.Entity())
	}
	for _, st := range q.Stages {
		if !displayStages[st.Kind] {
			return "", nil, fmt.Errorf("| %s turns mail into something else; a bulk action needs the mail itself", st.Kind)
		}
	}
	return query.CompileFilter(q.Filter, queryOptions(0, 0))
}
