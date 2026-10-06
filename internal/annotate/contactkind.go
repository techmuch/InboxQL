package annotate

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/laya"
	"github.com/user/inboxql/internal/store"
)

// KindResult is what a model pass over contacts did.
type KindResult struct {
	Examined  int64 `json:"examined"`
	System    int64 `json:"system"`
	Person    int64 `json:"person"`
	Undecided int64 `json:"undecided"`
	DryRun    bool  `json:"dryRun"`
}

// Cut-offs for writing a kind at all. Between them the contact is left
// unknown, which is a real answer: "not sure" stays visibly unsure rather than
// becoming a confident wrong one in every query that filters on kind.
//
// They are ranking cuts, not probabilities. The decision model ships
// uncalibrated, so "0.85" means "near the top of what it ranks as a system",
// not "right 85% of the time". Your ruling on the contact card always wins.
const (
	systemAbove = 0.85
	personBelow = 0.15
)

// kindSource is recorded beside a kind the model set, so a person's ruling —
// and the header classifier's near-certain evidence — can be told from it.
const kindSource = "laya"

// kindQuestion is asked of every unknown sender.
var kindQuestion = laya.Question{
	Kind:         "noul",
	Instructions: "this address belongs to an automated system or a company's mailer, not to a person",
}

// ClassifyContactsWithLaya judges the senders the header classifier left
// unknown, from their address and their last three subject lines.
//
// It runs after the header pass, never instead of it: headers are free and
// near-certain, and a model guessing at what a List-Unsubscribe header already
// states is paying for a worse answer.
func ClassifyContactsWithLaya(ctx context.Context, dataDir string, dryRun bool, limit int) (*KindResult, error) {
	m, err := laya.Open(dataDir)
	if err != nil {
		return nil, err
	}
	senders, err := store.UnknownSenders(limit)
	if err != nil {
		return nil, err
	}
	out := &KindResult{DryRun: dryRun}
	for _, addr := range senders {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		subjects, err := store.RecentSubjects(addr, 3)
		if err != nil {
			return out, err
		}
		d, err := m.Decide(kindText(addr, subjects), kindQuestion)
		if err != nil {
			return out, fmt.Errorf("judging %s: %w", addr, err)
		}
		out.Examined++

		yes := d.Probabilities[len(d.Probabilities)-1]
		kind := ""
		switch {
		case yes >= systemAbove:
			kind, out.System = store.KindSystem, out.System+1
		case yes <= personBelow:
			kind, out.Person = store.KindPerson, out.Person+1
		default:
			out.Undecided++
		}
		if kind != "" && !dryRun {
			if err := store.SetContactKind(addr, kind, kindSource); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// kindText is what the model reads about a contact.
func kindText(addr string, subjects []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\nRecent subjects:\n", addr)
	for _, s := range subjects {
		if strings.TrimSpace(s) == "" {
			s = "(no subject)"
		}
		fmt.Fprintf(&b, "- %s\n", s)
	}
	return b.String()
}
