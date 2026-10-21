package store

import (
	"strings"
	"sync"
)

// All Items: one search across every kind of thing.
//
// The query language answers about one kind at a time by design — that is
// what keeps a result precise — so this runs the same words once per kind and
// puts the answers side by side, rather than mixing them into one list. A
// term a kind does not understand drops that kind with the reason, instead of
// it silently finding nothing.

// SearchKinds are what All Items searches, in the order it shows them. The
// log is left out: it is about the application, not the mailbox.
var SearchKinds = []struct {
	ID, Label, Term string
}{
	{"mail", "Mail", ""},
	{"files", "Files", "in:attachments"},
	{"cards", "Cards", "in:tickets"},
	{"people", "People", "in:contacts"},
	{"events", "Events", "in:events"},
}

// SearchGroup is one kind's answer.
type SearchGroup struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Query is what "See all" opens: the kind's term and the words.
	Query string       `json:"query"`
	Total int64        `json:"total"`
	Items *QueryResult `json:"items,omitempty"`
	// Error is why this kind was left out, such as a term it has no
	// meaning for.
	Error string `json:"error,omitempty"`
}

// SearchAll runs words across every kind, at most limit of each.
func SearchAll(words string, limit int) []SearchGroup {
	words = strings.TrimSpace(words)
	out := make([]SearchGroup, len(SearchKinds))
	var wg sync.WaitGroup
	for i, k := range SearchKinds {
		q := strings.TrimSpace(k.Term + " " + words)
		out[i] = SearchGroup{ID: k.ID, Label: k.Label, Query: q}
		if words == "" {
			continue
		}
		wg.Add(1)
		go func(g *SearchGroup) {
			defer wg.Done()
			res, err := RunQuery(g.Query, limit, 0)
			if err != nil {
				g.Error = err.Error()
				return
			}
			n, err := CountQuery(g.Query)
			if err != nil {
				g.Error = err.Error()
				return
			}
			g.Items, g.Total = res, n
		}(&out[i])
	}
	wg.Wait()
	return out
}
