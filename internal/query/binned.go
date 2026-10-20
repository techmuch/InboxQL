package query

import (
	"path"
	"strings"
)

// Trash and Spam, the two places mail goes to be out of the way.
//
// # Why a query leaves them out
//
// Only the folder views used to: `folder:inbox` and `folder:allmail` excluded
// them, and every other query — `from:bob`, `anyone:x | count by day`, the
// dashboard with nothing applied — counted mail that had been thrown away. A
// contact's chart of how often they write included the newsletters you binned
// and the spam sent in their name. Gmail's search leaves both out unless the
// search asks for them, and so does this.
//
// # What asking looks like
//
// Mentioning a bin anywhere in the query — `folder:trash`, `label:spam`,
// `is:deleted`, inside an OR, after a `-`, inside a saved query — turns off
// the exclusion for that bin and only that one. `-folder:trash` is a mention
// too: it is redundant now, and reading it as "and include Spam" would be a
// surprise. `folder:all` mentions both, which is how "everywhere" is said.
//
// `id:` turns off both. It names messages exactly — a selection, an
// annotator run on one message — and a named message that is not found reads
// as a message that does not exist.
const (
	BinTrash = "trash"
	BinSpam  = "spam"
)

// Bins in the order they are reported.
var Bins = []string{BinTrash, BinSpam}

// binned records which bins a query mentioned, as it is compiled.
type binned struct {
	trash, spam bool
}

// note reads one term for a mention of a bin. Called on every term the
// compiler sees, including those of a saved query it inlines, which is why it
// is done during compilation and not by walking the parsed query.
func (b *binned) note(t *Term) {
	v := strings.ToLower(t.Value)
	switch t.Field {
	case "folder":
		switch v {
		case "trash":
			b.trash = true
		case "spam":
			b.spam = true
		case "all", "":
			b.trash, b.spam = true, true
		}
	case "label":
		if t.Op == OpGlob {
			if ok, _ := path.Match(v, BinTrash); ok {
				b.trash = true
			}
			if ok, _ := path.Match(v, BinSpam); ok {
				b.spam = true
			}
			return
		}
		switch v {
		case BinTrash:
			b.trash = true
		case BinSpam:
			b.spam = true
		}
	case "is":
		switch v {
		case "deleted", "trashed":
			b.trash = true
		case "junk", "spam":
			b.spam = true
		}
	case "id":
		b.trash, b.spam = true, true
	}
}

// leftOut lists the bins the query did not mention.
func (b binned) leftOut() []string {
	var out []string
	if !b.trash {
		out = append(out, BinTrash)
	}
	if !b.spam {
		out = append(out, BinSpam)
	}
	return out
}

// binsApply reports whether an entity's rows are mail that can be binned.
// Files are included: a file that arrived only in mail you threw away is not
// one of your files any more. Tickets, drafts, contacts, events and the log
// are not mail and keep their own meaning.
func binsApply(entity string) bool {
	return entity == EntityMessage || entity == EntityAttachment
}

// outside is the predicate over alias `m` for "in none of these bins".
func outside(bins []string) string {
	if len(bins) == 0 {
		return ""
	}
	parts := make([]string, len(bins))
	for i, b := range bins {
		parts[i] = "NOT " + mailLabelExists(b)
	}
	return "(" + strings.Join(parts, " AND ") + ")"
}

// inside is the opposite: in any of them. It is what the Desk counts to say
// how much a query left out.
func inside(bins []string) string {
	if len(bins) == 0 {
		return "(1=0)"
	}
	parts := make([]string, len(bins))
	for i, b := range bins {
		parts[i] = mailLabelExists(b)
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// Unbinned is "the message idCol names is in neither Trash nor Spam", for SQL
// that counts mail outside a query — a contact's counts, their responsiveness
// — and has to agree with what a query about the same mail would count. A card
// whose Messages figure included Trash, above a chart that did not, would be
// two answers to one question.
func Unbinned(idCol string) string {
	return "NOT EXISTS (SELECT 1 FROM message_labels bl WHERE bl.message_id = " + idCol +
		" AND bl.label IN ('" + BinTrash + "', '" + BinSpam + "'))"
}
