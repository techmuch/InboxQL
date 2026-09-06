package store

import (
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/query"
)

// maxCandidates caps a completion list.
//
// A dropdown nobody scrolls past ten items in does not benefit from five
// hundred, and the query that produces them is a scan when the prefix is short.
const maxCandidates = 20

// Complete answers what may be typed at a cursor position.
//
// The grammar half is [query.Complete], which needs no database; this fills in
// the candidates that come from the user's own mail — the addresses they
// actually correspond with, the annotators they defined, the folders their
// server has. That split is what keeps the parser free of a store dependency
// while still letting completion offer real values rather than syntax.
func Complete(src string, pos int) (*query.Completion, error) {
	c := query.Complete(src, pos)
	if c.ValueSource == "" {
		return c, nil
	}

	values, err := completionValues(c.ValueSource, c.Prefix)
	if err != nil {
		return nil, err
	}
	c.Candidates = append(c.Candidates, values...)
	return c, nil
}

func completionValues(source, prefix string) ([]query.Candidate, error) {
	switch source {
	case query.ValuesAddresses:
		return addressCandidates(prefix)
	case query.ValuesAccounts:
		return accountCandidates(prefix)
	case query.ValuesMailboxes:
		return mailboxCandidates(prefix)
	case query.ValuesAnnotators:
		return annotatorCandidates(prefix)
	case query.ValuesSaved:
		return savedCandidates(prefix)
	case query.ValuesFolders:
		out := []query.Candidate{}
		for _, f := range Folders {
			if strings.HasPrefix(f, strings.ToLower(prefix)) {
				out = append(out, query.Candidate{Value: f, Kind: "folder"})
			}
		}
		return out, nil
	}
	return nil, nil
}

// addressCandidates offers correspondents, most-corresponded-with first.
//
// Ordered by volume rather than alphabetically because the address someone is
// reaching for is overwhelmingly one they deal with often, and an alphabetical
// list buries it under mailing lists.
func addressCandidates(prefix string) ([]query.Candidate, error) {
	rows, err := db.Query(`
		SELECT p.address, COUNT(DISTINCT p.message_id) AS n
		FROM message_participants p
		WHERE (? = '' OR instr(p.address, ?) > 0)
		GROUP BY p.address
		ORDER BY n DESC, p.address ASC
		LIMIT ?`, strings.ToLower(prefix), strings.ToLower(prefix), maxCandidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []query.Candidate{}
	for rows.Next() {
		var addr string
		var n int64
		if err := rows.Scan(&addr, &n); err != nil {
			return nil, err
		}
		out = append(out, query.Candidate{
			Value: addr, Detail: plural(n, "message"), Kind: "address",
		})
	}
	return out, rows.Err()
}

func accountCandidates(prefix string) ([]query.Candidate, error) {
	rows, err := db.Query(`
		SELECT id, COALESCE(name, id) FROM accounts
		WHERE (? = '' OR instr(LOWER(id), ?) > 0)
		ORDER BY id LIMIT ?`, strings.ToLower(prefix), strings.ToLower(prefix), maxCandidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []query.Candidate{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out = append(out, query.Candidate{Value: id, Detail: name, Kind: "account"})
	}
	return out, rows.Err()
}

func mailboxCandidates(prefix string) ([]query.Candidate, error) {
	rows, err := db.Query(`
		SELECT mailbox, COUNT(*) AS n FROM messages
		WHERE mailbox IS NOT NULL AND mailbox != ''
		  AND (? = '' OR instr(LOWER(mailbox), ?) > 0)
		GROUP BY mailbox ORDER BY n DESC LIMIT ?`,
		strings.ToLower(prefix), strings.ToLower(prefix), maxCandidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []query.Candidate{}
	for rows.Next() {
		var name string
		var n int64
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		out = append(out, query.Candidate{
			Value: name, Detail: plural(n, "message"), Kind: "mailbox",
		})
	}
	return out, rows.Err()
}

// annotatorCandidates offers labels and extractors, with their coverage.
//
// The detail is the coverage rather than the match count, because the useful
// thing to know before filtering on a label is how much of the mailbox that
// annotator has actually seen.
func annotatorCandidates(prefix string) ([]query.Candidate, error) {
	annotators, err := ListAnnotators()
	if err != nil {
		return nil, err
	}

	out := []query.Candidate{}
	for _, a := range annotators {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(a.Name), strings.ToLower(prefix)) {
			continue
		}
		detail := a.Kind
		if p, err := Progress(a, ""); err == nil {
			detail = fmt.Sprintf("%s · %d/%d evaluated", a.Kind, p.Evaluated, p.Total)
		}
		out = append(out, query.Candidate{Value: a.Name, Detail: detail, Kind: "annotator"})
	}
	return out, nil
}

func savedCandidates(prefix string) ([]query.Candidate, error) {
	queries, err := ListSavedQueries()
	if err != nil {
		return nil, err
	}

	out := []query.Candidate{}
	for _, q := range queries {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(q.Name), strings.ToLower(prefix)) {
			continue
		}
		out = append(out, query.Candidate{Value: q.Name, Detail: q.Title, Kind: "saved"})
	}
	return out, nil
}

// plural renders a count with its noun, for the detail line beside a candidate.
func plural(n int64, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// FieldValues lists candidate values for one field, for a client that wants a
// picker rather than an inline completion.
func FieldValues(field, prefix string) ([]query.Candidate, error) {
	f, ok := query.LookupField(field)
	if !ok {
		return nil, nil
	}

	out := []query.Candidate{}
	for _, v := range f.Enum {
		if strings.HasPrefix(v, strings.ToLower(prefix)) {
			out = append(out, query.Candidate{Value: v, Kind: "value"})
		}
	}
	if f.Values == "" {
		return out, nil
	}

	values, err := completionValues(f.Values, prefix)
	if err != nil {
		return nil, err
	}
	return append(out, values...), nil
}
