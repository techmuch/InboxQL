// Package work hands conversations to an AI harness and takes its answers.
//
// # The loop
//
//	iql --json work next --limit 10     what to read, and what to answer
//	iql --json work submit < answer.json  the answers, one conversation each
//
// The harness reads each conversation once and answers every element pending
// on it (internal/elements): what was bought, travel, events, tasks, who is
// waiting, how much it matters, what kind of sender someone is, its topics.
// Everything a person or a later query needs to trust those answers is kept
// with them: the model, a confidence, why, and a quote checked against the
// conversation.
//
// # What is pending
//
// An element is pending on a conversation when it has no answer at its
// current version: for a conversation element, on the newest message — so a
// reply makes `waiting` pending again, and not `receipts` on the messages
// before it; for a message element, on each message; for `sender`, on each
// sender not yet classified. Your rulings are answers, at any version.
package work

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
)

// LeaseFor is how long a conversation stays with a harness that took it.
const LeaseFor = 30 * time.Minute

// MaxMessages is how many of a conversation's messages are handed over: the
// newest, which is what every element is about. Older ones are counted.
const MaxMessages = 25

// Ask is one element to answer on a conversation.
type Ask struct {
	Element string `json:"element"`
	Version int    `json:"version"`
	Unit    string `json:"unit"`
	// Messages are the messages to answer about: the newest alone for a
	// conversation element.
	Messages []string `json:"messages,omitempty"`
	// Senders are the addresses to classify, for `sender`.
	Senders []string `json:"senders,omitempty"`
}

// Person is someone in the conversation and what is known about them.
type Person struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
	Kind    string `json:"kind"`
	Me      bool   `json:"me,omitempty"`
}

// Item is one conversation to read.
type Item struct {
	Thread   string                  `json:"thread"`
	Lease    string                  `json:"lease"`
	Expires  time.Time               `json:"expires"`
	Subject  string                  `json:"subject"`
	Messages []*store.ReadingMessage `json:"messages"`
	// Earlier counts messages older than those handed over.
	Earlier int      `json:"earlier,omitempty"`
	People  []Person `json:"people"`
	Ask     []Ask    `json:"ask"`
}

// Batch is what `work next` returns.
type Batch struct {
	// Elements are the definitions of everything asked in this batch, once.
	Elements map[string]elements.Element `json:"elements"`
	// Topics are the topics already in use, when topics are asked.
	Topics []string `json:"topics,omitempty"`
	Items  []*Item  `json:"items"`
	// Submit says how to answer.
	Submit string `json:"submit"`
}

// Options narrow what `work next` hands out.
type Options struct {
	Limit int
	// Only names the elements to ask; empty asks all that are installed.
	Only []string
	// Query narrows the conversations to those with mail it matches.
	Query string
}

// scanBudget bounds how many conversations one call looks through for work,
// so a mailbox where everything recent is answered returns promptly.
const scanBudget = 2000

// Next finds the newest conversations with something to answer, and leases
// them to the caller.
func Next(opt Options) (*Batch, error) {
	if opt.Limit <= 0 {
		opt.Limit = 10
	}
	installed, byName, err := elements.Installed()
	if err != nil {
		return nil, err
	}
	var els []elements.Element
	for _, e := range installed {
		if len(opt.Only) == 0 || has(opt.Only, e.Name) {
			els = append(els, e)
		}
	}
	for _, n := range opt.Only {
		if _, ok := byName[n]; !ok {
			return nil, fmt.Errorf("no element named %q is installed and on (`iql elements install %s`)", n, n)
		}
	}
	if len(els) == 0 {
		return nil, fmt.Errorf("no elements are installed: `iql elements install` adds the catalog")
	}

	b := &Batch{Elements: map[string]elements.Element{}, Items: []*Item{},
		Submit: "iql --json work submit < answers.json — one answer per item; `iql work guide` for the shape"}
	const page = 50
	for skip := 0; skip < scanBudget && len(b.Items) < opt.Limit; skip += page {
		keys, err := store.RecentThreads(opt.Query, skip, page)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if len(b.Items) >= opt.Limit {
				break
			}
			if l, err := store.LeaseFor(k); err != nil {
				return nil, err
			} else if l != nil && l.Expires.After(time.Now()) {
				continue
			}
			item, err := pending(k, els, byName)
			if err != nil {
				return nil, err
			}
			if item == nil {
				continue
			}
			asked, _ := json.Marshal(item.Ask)
			l, err := store.TakeLease(k, string(asked), LeaseFor)
			if err != nil {
				continue // taken by another run between the check and now
			}
			item.Lease, item.Expires = l.Token, l.Expires
			for _, a := range item.Ask {
				b.Elements[a.Element] = elementFor(a.Element, els)
			}
			b.Items = append(b.Items, item)
		}
		if len(keys) < page {
			break
		}
	}
	if _, ok := b.Elements["topics"]; ok {
		if b.Topics, err = store.KnownTopics(80); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// pending builds the item for a conversation, or nil when nothing is pending.
func pending(key string, els []elements.Element, byName map[string]*store.Annotator) (*Item, error) {
	msgs, err := store.ThreadReading(key)
	if err != nil || len(msgs) == 0 {
		return nil, err
	}
	item := &Item{Thread: key}
	if len(msgs) > MaxMessages {
		item.Earlier = len(msgs) - MaxMessages
		msgs = msgs[len(msgs)-MaxMessages:]
	}
	item.Messages = msgs
	newest := msgs[len(msgs)-1]
	item.Subject = newest.Subject

	for _, e := range els {
		a := byName[e.Name]
		ask := Ask{Element: e.Name, Version: a.Version, Unit: e.Unit}
		switch e.Unit {
		case elements.UnitThread:
			ok, err := needs(a, newest.ID)
			if err != nil {
				return nil, err
			}
			if ok {
				ask.Messages = []string{newest.ID}
			}
		case elements.UnitMessage:
			for _, m := range msgs {
				ok, err := needs(a, m.ID)
				if err != nil {
					return nil, err
				}
				if ok {
					ask.Messages = append(ask.Messages, m.ID)
				}
			}
		case elements.UnitSender:
			for _, s := range senders(msgs) {
				c, err := store.GetContact(s)
				if err != nil {
					return nil, err
				}
				if c == nil || c.Kind == store.KindUnknown {
					ask.Senders = append(ask.Senders, s)
				}
			}
		}
		if len(ask.Messages) > 0 && a.Scope != "" {
			if ask.Messages, err = store.MatchingIDs(a.Scope, ask.Messages); err != nil {
				return nil, fmt.Errorf("%s scope: %w", e.Name, err)
			}
		}
		if len(ask.Messages) > 0 || len(ask.Senders) > 0 {
			item.Ask = append(item.Ask, ask)
		}
	}
	if len(item.Ask) == 0 {
		return nil, nil
	}
	item.People, err = people(msgs)
	return item, err
}

func needs(a *store.Annotator, messageID string) (bool, error) {
	done, err := store.Answered(a, messageID)
	return !done, err
}

// senders are the addresses that wrote in the conversation, other than you.
func senders(msgs []*store.ReadingMessage) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range msgs {
		if m.Mine {
			continue
		}
		a, _ := store.NormaliseAddress(m.From)
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

func people(msgs []*store.ReadingMessage) ([]Person, error) {
	seen := map[string]bool{}
	var out []Person
	add := func(raw string, mine bool) error {
		a, _ := store.NormaliseAddress(raw)
		if a == "" || seen[a] {
			return nil
		}
		seen[a] = true
		p := Person{Address: a, Kind: store.KindUnknown, Me: mine}
		c, err := store.GetContact(a)
		if err != nil {
			return err
		}
		if c != nil {
			p.Kind = c.Kind
			p.Name = strings.TrimSpace(firstNonEmpty(c.DisplayName, c.HeaderName))
		}
		out = append(out, p)
		return nil
	}
	for _, m := range msgs {
		if err := add(m.From, m.Mine); err != nil {
			return nil, err
		}
		for _, r := range append(append([]string{}, m.To...), m.Cc...) {
			if err := add(r, false); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func elementFor(name string, els []elements.Element) elements.Element {
	for _, e := range els {
		if e.Name == name {
			return e
		}
	}
	return elements.Element{}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}
