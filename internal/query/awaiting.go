package query

import "strings"

// Waiting for a reply, defined once.
//
// # Why here
//
// The rail's "Waiting on me" (`in:contacts awaiting:me`) and a contact card's
// "Awaiting your reply" were two answers to one question; both now read the
// SQL below.
//
// # The definition
//
// Waiting is a judgement: many messages are sent and nobody is waiting on
// anybody, "Thanks, sounds good!" closes a conversation, and "I'll get back to
// you Monday" means you owe the next message even though you wrote last. No
// rule about who sent last gets that right, so nothing here guesses.
//
// A conversation is waiting when the `waiting` element (internal/elements) has
// judged its newest message outside Trash and Spam — your ruling first, then
// the harness's or a model's at the current version — as on-me or on-them. A
// conversation nobody has judged is not waiting; `unlabeled:waiting` finds
// those.

// WaitingAnnotator is the element that judges who a conversation waits on.
const WaitingAnnotator = "waiting"

// Directions a conversation can wait in, and the judgement that it does not.
const (
	AwaitingMe   = "me"
	AwaitingThem = "them"

	levelOnMe   = "on-me"
	levelOnThem = "on-them"
	levelNoOne  = "no-one"
)

// SQLList renders values as a quoted SQL list, "('a', 'b')", or "" for none.
func SQLList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = SQLString(strings.ToLower(strings.TrimSpace(v)))
	}
	return "(" + strings.Join(q, ", ") + ")"
}

// SQLString quotes one value as an SQL string literal.
func SQLString(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

// WaitingLevel is the `waiting` judgement of a message — on-me, on-them,
// no-one — or NULL when nothing has judged it: a person's ruling, then an
// answer at the element's current version.
func WaitingLevel(msgID string) string {
	return "(SELECT json_extract(an.data_json, '$.level') FROM annotations an JOIN annotators a ON a.id = an.annotator_id" +
		" WHERE a.name = '" + WaitingAnnotator + "' AND an.message_id = " + msgID +
		" AND an.status IN ('ok', 'empty')" +
		" AND (an.source = 'human' OR an.annotator_version = a.version)" +
		" ORDER BY CASE WHEN an.source = 'human' THEN 0 ELSE 1 END, an.seq LIMIT 1)"
}

// newest is true of l when it is the newest message, outside Trash and Spam,
// of its conversation.
func newest(l string) string {
	return Unbinned(l+".id") + " AND " + l + ".date = (SELECT MAX(x.date) FROM messages x" +
		" WHERE x.thread_key = " + l + ".thread_key AND " + Unbinned("x.id") + ")"
}

func judged(dir, l string) string {
	level := levelOnMe
	switch dir {
	case AwaitingThem:
		level = levelOnThem
	case levelNoOne:
		level = levelNoOne
	}
	return "(" + newest(l) + " AND COALESCE(" + WaitingLevel(l+".id") + ", '') = '" + level + "')"
}

// Waiting is true of l when it is the newest message of a conversation
// judged to wait on dir. On mail, `awaiting:me` is this.
func Waiting(dir, l string) string { return judged(dir, l) }

// NobodyWaiting is true of l when its conversation was judged to wait on
// no one: what a contact card reports it left out.
func NobodyWaiting(l string) string { return judged(levelNoOne, l) }

// Involves narrows a conversation to one contact: for one waiting on me,
// they wrote in it; otherwise, they are in it. Your own addresses are never
// the contact (selfs, from SQLList). contact is an SQL expression for the
// address.
func Involves(dir, l, contact, selfs string) string {
	role := ""
	if dir == AwaitingMe {
		role = " AND tp.role = 'from'"
	}
	notMe := ""
	if selfs != "" {
		notMe = contact + " NOT IN " + selfs + " AND "
	}
	return "(" + notMe + "EXISTS (SELECT 1 FROM messages t JOIN message_participants tp ON tp.message_id = t.id" + role +
		" WHERE t.thread_key = " + l + ".thread_key AND tp.address = " + contact +
		" AND " + Unbinned("t.id") + "))"
}

// ContactAwaiting is true of contact c when a conversation they are part of
// waits on dir. Driven from their own messages so the planner starts from an
// index rather than every conversation.
func ContactAwaiting(dir, contact, selfs string) string {
	return "EXISTS (SELECT 1 FROM message_participants cp JOIN messages c0 ON c0.id = cp.message_id" +
		" JOIN messages l ON l.thread_key = c0.thread_key" +
		" WHERE cp.address = " + contact + " AND " + Waiting(dir, "l") +
		" AND " + Involves(dir, "l", contact, selfs) + ")"
}

// ImportanceAnnotator is the element that judges how much a conversation
// matters.
const ImportanceAnnotator = "importance"

// ElementLevel is a message's level from a levelled element — a person's
// ruling, then an answer at the element's current version — or NULL.
func ElementLevel(element, msgID string) string {
	return "(SELECT json_extract(an.data_json, '$.level') FROM annotations an JOIN annotators a ON a.id = an.annotator_id" +
		" WHERE a.name = " + SQLString(element) + " AND an.message_id = " + msgID +
		" AND an.status = 'ok'" +
		" AND (an.source = 'human' OR an.annotator_version = a.version)" +
		" ORDER BY CASE WHEN an.source = 'human' THEN 0 ELSE 1 END, an.seq LIMIT 1)"
}

// ConversationLevel is the level a conversation was last judged at by a
// conversation element: the judgement on its newest judged message. Every
// message of the conversation carries it, so "archive the low-importance
// mail" archives whole conversations rather than their newest message only.
func ConversationLevel(element, l string) string {
	return "(SELECT lvl FROM (SELECT " + ElementLevel(element, "j.id") + " AS lvl, j.date AS d FROM messages j" +
		" WHERE (j.thread_key = " + l + ".thread_key OR j.id = " + l + ".id)) WHERE lvl IS NOT NULL ORDER BY d DESC LIMIT 1)"
}
