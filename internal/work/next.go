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
	"regexp"
	"sort"
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
	// Flag is why this conversation was marked to be read again properly,
	// when it was.
	Flag string `json:"flag,omitempty"`
	// Blank names the elements the template left for you to answer rather
	// than guessing, and SuggestedLabels what the subject suggests — set by
	// Template, shown by Digest.
	Blank           []string `json:"blank,omitempty"`
	SuggestedLabels []string `json:"suggestedLabels,omitempty"`
}

// Batch is what `work next` returns.
type Batch struct {
	// ID names the batch, so what reading it cost can be reported against it.
	ID string `json:"batch,omitempty"`
	// Kind is read, deep or audit.
	Kind string `json:"kind,omitempty"`
	// Elements are the definitions of everything asked in this batch, once.
	Elements map[string]elements.Element `json:"elements"`
	// Topics are the topics already in use, when topics are asked.
	Topics []string `json:"topics,omitempty"`
	Items  []*Item  `json:"items"`
	// Submit says how to answer.
	Submit string `json:"submit"`
	// Exhausted is set when the scan stopped at its budget before filling the
	// batch: older conversations may still be waiting, reachable with --query.
	Exhausted bool `json:"exhausted,omitempty"`
	// ChecksDue is how many conversations should be checked now to keep the
	// measured accuracy honest (see `iql work audit`).
	ChecksDue int `json:"checksDue,omitempty"`
}

// Options narrow what `work next` hands out.
type Options struct {
	Limit int
	// Only names the elements to ask; empty asks all that are installed.
	Only []string
	// Query narrows the conversations to those with mail it matches.
	Query string
	// Deep reads properly what was only guessed: conversations flagged for it
	// first, then answers that went back exactly as the template pre-filled
	// them. Its template guesses nothing.
	Deep bool
	// Model is who will answer, recorded with the batch.
	Model string
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

	kind := store.WorkKindRead
	if opt.Deep {
		kind = store.WorkKindDeep
	}
	b := &Batch{ID: store.NewWorkBatchID(), Kind: kind, Elements: map[string]elements.Element{}, Items: []*Item{},
		Submit: "iql --json work submit < answers.json — one answer per item; `iql work guide` for the shape"}
	seen := map[string]bool{}
	take := func(k string, m mode, flag string) error {
		if seen[k] {
			return nil
		}
		seen[k] = true
		if l, err := store.LeaseFor(k); err != nil {
			return err
		} else if l != nil && l.Expires.After(time.Now()) {
			return nil
		}
		item, err := pending(k, els, byName, m)
		if err != nil || item == nil {
			return err
		}
		item.Flag = flag
		asked, _ := json.Marshal(item.Ask)
		l, err := store.TakeLeaseIn(k, string(asked), b.ID, LeaseFor)
		if err != nil {
			return nil // taken by another run between the check and now
		}
		item.Lease, item.Expires = l.Token, l.Expires
		for _, a := range item.Ask {
			b.Elements[a.Element] = elementFor(a.Element, els)
		}
		b.Items = append(b.Items, item)
		return nil
	}

	// A deep read starts with what a check or a person flagged.
	if opt.Deep {
		flags, err := store.FlaggedThreads(opt.Limit * 4)
		if err != nil {
			return nil, err
		}
		for _, f := range flags {
			if len(b.Items) >= opt.Limit {
				break
			}
			if opt.Query != "" {
				if ok, err := store.ThreadMatches(f.Thread, opt.Query); err != nil || !ok {
					continue
				}
			}
			if err := take(f.Thread, modeAll, f.Reason); err != nil {
				return nil, err
			}
		}
	}

	m := modeRead
	if opt.Deep {
		m = modeDeep
	}
	const page = 50
	skip := 0
	for ; skip < scanBudget && len(b.Items) < opt.Limit; skip += page {
		keys, err := store.RecentThreads(opt.Query, skip, page)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if len(b.Items) >= opt.Limit {
				break
			}
			if err := take(k, m, ""); err != nil {
				return nil, err
			}
		}
		if len(keys) < page {
			skip = -1
			break
		}
	}
	b.Exhausted = skip >= scanBudget && len(b.Items) < opt.Limit
	if len(b.Items) > 0 {
		if _, err := store.CreateWorkBatch(b.ID, kind, opt.Model, len(b.Items)); err != nil {
			return nil, err
		}
	} else {
		b.ID = ""
	}
	if r, err := LoadRouting(); err == nil {
		b.ChecksDue, _ = r.ChecksDue()
	}
	if _, ok := b.Elements["topics"]; ok {
		if b.Topics, err = store.KnownTopics(80); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// mode is how much of a conversation counts as still to answer.
type mode int

const (
	// modeRead: what has no answer at all.
	modeRead mode = iota
	// modeDeep: also what was answered only by an untouched template guess.
	modeDeep
	// modeAll: everything no person has ruled on — a flagged conversation is
	// read again whole.
	modeAll
)

// pending builds the item for a conversation, or nil when nothing is pending.
func pending(key string, els []elements.Element, byName map[string]*store.Annotator, m mode) (*Item, error) {
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
			ok, err := needs(a, newest.ID, m)
			if err != nil {
				return nil, err
			}
			if ok {
				ask.Messages = []string{newest.ID}
			}
		case elements.UnitMessage:
			for _, msg := range msgs {
				ok, err := needs(a, msg.ID, m)
				if err != nil {
					return nil, err
				}
				if ok {
					ask.Messages = append(ask.Messages, msg.ID)
				}
			}
		case elements.UnitSender:
			for _, s := range senders(msgs) {
				c, err := store.GetContactSummary(s)
				if err != nil {
					return nil, err
				}
				if c == nil || c.Kind == store.KindUnknown || (m == modeAll && c.KindSource != store.ContactFromHuman) {
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

func needs(a *store.Annotator, messageID string, m mode) (bool, error) {
	if m == modeAll {
		ruled, err := store.HumanRuled(a, messageID)
		return !ruled, err
	}
	done, err := store.AnsweredBy(a, messageID, m == modeDeep)
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
		c, err := store.GetContactSummary(a)
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

// Template builds a pre-scaffolded slice of Submissions for a Batch, with all
// thread keys, lease tokens, message IDs and sender addresses filled in, and a
// guess for the elements the routing says may be guessed, so a harness only
// edits what differs.
//
// # What is not guessed
//
// A guess left untouched is stored as the answer, so a template guesses only
// where guessing has been measured to be safe enough:
//
//   - nothing at all in a deep read or a flagged conversation;
//   - no element whose checked accuracy misses its target (LoadRouting),
//     unless the sender has a long record of being answered right;
//   - no "none" for an element the conversation looks like it holds — a
//     receipt-looking subject does not get receipts: none;
//   - no mail labels: a label is written to the server, so a keyword guess
//     must not reach it untouched. They are shown as suggestions instead.
//
// What is not guessed is left null, and a null answer is refused, so it has
// to be read. What was guessed is kept with the lease, so the submission can
// say which answers went back unchanged.
func Template(b *Batch, model string) []Submission {
	if strings.TrimSpace(model) == "" {
		model = "agent"
	}
	route, err := LoadRouting()
	if err != nil {
		route = &Routing{Elements: map[string]*ElementRoute{}}
	}
	senderCache := map[string]store.SenderHistory{}
	lookupSender := func(it *Item) (string, store.SenderHistory) {
		snds := senders(it.Messages)
		if len(snds) == 0 {
			return "", store.SenderHistory{}
		}
		addr := snds[len(snds)-1]
		if h, ok := senderCache[addr]; ok {
			return addr, h
		}
		h, _ := store.SenderDefaults(addr)
		senderCache[addr] = h
		return addr, h
	}

	out := make([]Submission, 0, len(b.Items))
	for _, it := range b.Items {
		addr, hist := lookupSender(it)
		// A deep read and a check are there to read, so nothing is guessed:
		// a check that started from the first read's guesses would agree
		// with them and prove nothing.
		guess := (b.Kind == "" || b.Kind == store.WorkKindRead) && it.Flag == ""
		sub := defaultSubmissionFor(it, b.Elements, model, hist, route, addr, guess)
		if prefill, err := json.Marshal(sub.Answers); err == nil {
			_ = store.SetLeasePrefill(it.Thread, it.Lease, string(prefill))
		}
		out = append(out, sub)
	}
	return out
}

// signalFor names the candidate signal that says a conversation probably
// holds an element, so the template does not guess "none" for it.
var signalFor = map[string]string{"receipts": "receipt", "travel": "travel", "events": "event", "tasks": "task"}

var nullAnswer = json.RawMessage("null")

func defaultSubmissionFor(it *Item, elementsMap map[string]elements.Element, model string,
	hist store.SenderHistory, route *Routing, sender string, guess bool) Submission {
	defQuote := defaultQuote(it.Messages)
	allSystem := isSystemThread(it)
	sigs := candidateSignals(it)
	trusted := route.Trusted(sender)
	ans := map[string]json.RawMessage{}
	it.Blank = nil
	for _, a := range it.Ask {
		el := elementsMap[a.Element]
		if !guess || !route.MayGuess(a.Element, trusted) || (signalFor[a.Element] != "" && has(sigs, signalFor[a.Element])) {
			ans[a.Element] = nullAnswer
			it.Blank = append(it.Blank, a.Element)
			continue
		}
		var v any
		switch el.Unit {
		case elements.UnitMessage:
			m := map[string]Answer{}
			for _, mid := range a.Messages {
				m[mid] = Answer{None: "none"}
			}
			v = m
		case elements.UnitSender:
			m := map[string]Answer{}
			for _, s := range a.Senders {
				m[s] = Answer{Level: guessSenderLevelForItem(s, it)}
			}
			v = m
		default: // UnitThread
			switch el.Shape {
			case elements.ShapeRecords:
				v = Answer{None: "none"}
			case elements.ShapeTags:
				tags := hist.Topics
				if len(tags) == 0 {
					tags, _ = inferTopicsAndLabels(it)
				}
				if len(tags) > 0 {
					v = Answer{Tags: tags}
				} else {
					v = Answer{None: "general"}
				}
			case elements.ShapeLevels:
				lvl := defaultLevelFor(el, it, hist, allSystem)
				v = Answer{Level: lvl, Quote: defQuote}
			}
		}
		raw, _ := json.Marshal(v)
		ans[a.Element] = raw
	}
	lbls := hist.Labels
	if len(lbls) == 0 {
		_, lbls = inferTopicsAndLabels(it)
	}
	it.SuggestedLabels = lbls
	return Submission{
		Thread:  it.Thread,
		Lease:   it.Lease,
		Model:   model,
		Labels:  []string{},
		Answers: ans,
	}
}

func unquotedText(s string) string {
	var sb strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		sb.WriteString(trimmed)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func defaultLevelFor(el elements.Element, it *Item, hist store.SenderHistory, allSystem bool) string {
	// Waiting element: check turn-taking in human threads
	if el.HasLevel("no-one") {
		if !allSystem && len(it.Messages) > 0 {
			newest := it.Messages[len(it.Messages)-1]
			if newest.Mine && strings.Contains(unquotedText(newest.Text), "?") && el.HasLevel("on-them") {
				return "on-them"
			}
		}
		return "no-one"
	}
	// Subject-level urgency override: even if a sender usually sends low-importance
	// notifications, an urgent subject ("action required", "security alert", "fraud",
	// "appointment") upgrades the default level so it isn't buried.
	subj := strings.ToLower(it.Subject)
	if el.HasLevel("important") && (strings.Contains(subj, "action required") ||
		strings.Contains(subj, "urgent") ||
		strings.Contains(subj, "fraud") ||
		strings.Contains(subj, "past due") ||
		strings.Contains(subj, "appointment")) {
		return "important"
	}
	if hist.Importance != "" && el.HasLevel(hist.Importance) {
		return hist.Importance
	}
	if allSystem && el.HasLevel("low") {
		return "low"
	}
	if el.HasLevel("normal") {
		return "normal"
	}
	if len(el.Levels) > 0 {
		return el.Levels[0].Label
	}
	return ""
}

func inferTopicsAndLabels(it *Item) (topics []string, labels []string) {
	subj := strings.ToLower(it.Subject)
	var addr string
	if snds := senders(it.Messages); len(snds) > 0 {
		addr = strings.ToLower(snds[len(snds)-1])
	}
	hasCal := false
	automated := false
	for _, m := range it.Messages {
		if m.HasCalendar {
			hasCal = true
		}
		if m.Automated {
			automated = true
		}
	}
	switch {
	case strings.Contains(subj, "receipt") || strings.Contains(subj, "order #") || strings.Contains(subj, "invoice") ||
		strings.HasPrefix(subj, "ordered:") || strings.HasPrefix(subj, "ordered "):
		return []string{"receipt"}, []string{"receipts"}
	case strings.Contains(subj, "shipped:") || strings.Contains(subj, "shipped ") ||
		strings.Contains(subj, "delivered:") || strings.Contains(subj, "delivered ") ||
		strings.Contains(subj, "out for delivery"):
		return []string{"shipping", "shopping"}, []string{"shopping"}
	case strings.Contains(subj, "security alert") || strings.Contains(subj, "verification code") ||
		strings.Contains(subj, "sign-in") || strings.Contains(subj, "two-factor") || strings.Contains(subj, "password reset"):
		return []string{"security"}, []string{"security"}
	case strings.Contains(subj, "statement") || strings.Contains(subj, "credit report") ||
		strings.Contains(subj, "credit alert") || strings.Contains(subj, "deposit notification") ||
		strings.Contains(subj, "withdrawal notification") || strings.Contains(subj, "payment scheduled"):
		return []string{"finance"}, []string{"finance"}
	case strings.Contains(subj, "appointment") || strings.Contains(subj, "patient") ||
		strings.Contains(subj, "radiology") || strings.Contains(subj, "clinic") || strings.Contains(subj, "hospital"):
		return []string{"health"}, []string{"health"}
	case hasCal || strings.Contains(subj, "invitation:") || strings.Contains(subj, "agenda"):
		return []string{"events"}, []string{"events"}
	case strings.Contains(addr, "facebookmail.com") || strings.Contains(addr, "linkedin.com") ||
		strings.Contains(subj, "shared a new reel") || strings.Contains(subj, "added a new reel"):
		return []string{"social"}, []string{"social"}
	case strings.Contains(subj, "discount") || strings.Contains(subj, "flash sale") ||
		strings.Contains(subj, "% off") || strings.Contains(subj, "save ") || strings.Contains(subj, "deal"):
		return []string{"promotions"}, []string{"promotions"}
	case automated && (strings.Contains(addr, "newsletter") || strings.Contains(addr, "substack.com") || strings.Contains(subj, "digest") || strings.Contains(subj, "newsletter")):
		return []string{"newsletter"}, []string{"newsletters"}
	}
	return nil, nil
}

// defaultQuote picks a clean, verified substring from the newest message's
// subject (or first readable line of text) so routine level answers in
// Template already carry a valid quote from the conversation corpus.
func defaultQuote(msgs []*store.ReadingMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	corpus := corpusOf(msgs)
	newest := msgs[len(msgs)-1]
	if s := strings.TrimSpace(newest.Subject); len(s) >= 4 && quoted(s, corpus) == "" {
		return s
	}
	for _, line := range strings.Split(newest.Text, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 6 && !strings.HasPrefix(line, "http") {
			if len(line) > 60 {
				line = strings.TrimSpace(line[:60])
			}
			if quoted(line, corpus) == "" {
				return line
			}
		}
	}
	// Fallback for ultra-short messages ("Hi", "Ok")
	if s := strings.TrimSpace(newest.Subject); s != "" && quoted(s, corpus) == "" {
		return s
	}
	for _, line := range strings.Split(newest.Text, "\n") {
		if line = strings.TrimSpace(line); line != "" && quoted(line, corpus) == "" {
			return line
		}
	}
	return ""
}

func isSystemThread(it *Item) bool {
	for _, m := range it.Messages {
		if m.Automated {
			return true
		}
	}
	snds := senders(it.Messages)
	if len(snds) == 0 {
		return false
	}
	kindByAddr := map[string]string{}
	for _, p := range it.People {
		kindByAddr[p.Address] = p.Kind
	}
	for _, s := range snds {
		k := kindByAddr[s]
		if k == "" || k == store.KindUnknown {
			k = guessSenderLevelForItem(s, it)
		}
		if k != "system" {
			return false
		}
	}
	return true
}

func guessSenderLevelForItem(addr string, it *Item) string {
	if it == nil {
		return guessSenderLevel(addr)
	}
	return guessSenderLevelForMessages(addr, it.Messages)
}

func guessSenderLevelForMessages(addr string, msgs []*store.ReadingMessage) string {
	lvl := guessSenderLevel(addr)
	if lvl == "system" {
		return lvl
	}
	for _, m := range msgs {
		if m.Automated {
			return "system"
		}
	}
	return lvl
}

func guessSenderLevel(addr string) string {
	a := strings.ToLower(strings.TrimSpace(addr))
	for _, kw := range []string{
		"noreply", "no-reply", "do-not-reply", "donotreply",
		"mailer-daemon", "postmaster", "notifications@", "notification@",
		"alerts@", "alert@", "receipts@", "receipt@", "notice@",
		"updates@", "friendupdates@", "order-update@", "shipment-tracking@",
		"auto-confirm@", "newsletter@", "news@", "digest@",
		"automated", "bounce",
		"confirm@", "calendar-notification@",
		"@facebookmail.com", "@email.amazonses.com", "@us-west-2.amazonses.com",
		"@notifications.google.com",
	} {
		if strings.Contains(a, kw) {
			return "system"
		}
	}
	// A shared mailbox people answer: a business, not a person and not a
	// machine.
	if at := strings.IndexByte(a, '@'); at > 0 {
		switch a[:at] {
		case "info", "service", "support", "help", "billing", "invoicing", "accounts", "sales",
			"hello", "contact", "team", "office", "registration", "admissions", "reservations":
			return "organization"
		}
	}
	return "person"
}

func formatFieldsInline(fields []elements.Field) string {
	flds := make([]string, len(fields))
	for i, f := range fields {
		req := ""
		if f.Required {
			req = "*"
		}
		typ := f.Type
		if f.Type == elements.TypeList && len(f.Of) > 0 {
			typ = "list<{" + formatFieldsInline(f.Of) + "}>"
		}
		flds[i] = f.Name + req + ":" + typ
	}
	return strings.Join(flds, ", ")
}

func writeElementsSummary(sb *strings.Builder, elementsMap map[string]elements.Element) {
	if len(elementsMap) == 0 {
		return
	}
	names := make([]string, 0, len(elementsMap))
	for k := range elementsMap {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		el := elementsMap[k]
		fmt.Fprintf(sb, "  - %s (unit=%s, shape=%s)", el.Name, el.Unit, el.Shape)
		if len(el.Levels) > 0 {
			lvls := make([]string, len(el.Levels))
			for i, l := range el.Levels {
				lvls[i] = l.Label
			}
			fmt.Fprintf(sb, " levels=[%s]", strings.Join(lvls, "|"))
		}
		if len(el.Fields) > 0 {
			fmt.Fprintf(sb, " fields={%s}", formatFieldsInline(el.Fields))
		}
		sb.WriteString("\n")
	}
}

func candidateSignals(it *Item) []string {
	subj := strings.ToLower(it.Subject)
	var bodyBuilder strings.Builder
	hasCal := false
	for _, m := range it.Messages {
		bodyBuilder.WriteString(strings.ToLower(m.Text))
		bodyBuilder.WriteByte('\n')
		if m.HasCalendar {
			hasCal = true
		}
	}
	body := bodyBuilder.String()

	var signals []string
	// Human / direct person conversation signal: human emails rarely use rigid
	// B2C keywords when asking for deliverables or scheduling meetings.
	if !isSystemThread(it) && len(senders(it.Messages)) > 0 {
		signals = append(signals, "person")
	}
	// Receipts signals
	if strings.Contains(subj, "receipt") ||
		strings.HasPrefix(subj, "ordered ") ||
		strings.HasPrefix(subj, "ordered:") ||
		strings.Contains(subj, "order #") ||
		strings.Contains(subj, "invoice") ||
		strings.Contains(body, "grand total:") ||
		strings.Contains(body, "amount paid") ||
		strings.Contains(body, "you paid $") ||
		(strings.Contains(body, "total:") && strings.Contains(body, "order number:")) {
		signals = append(signals, "receipt")
	}
	// Events signals
	if hasCal ||
		strings.Contains(subj, "appointment") ||
		strings.Contains(subj, "agenda") ||
		strings.Contains(subj, "invitation") ||
		strings.Contains(body, "appointment details") ||
		strings.Contains(body, "add to calendar") {
		signals = append(signals, "event")
	}
	// Travel signals
	if strings.Contains(subj, "reservation") ||
		strings.Contains(subj, "itinerary") ||
		strings.Contains(subj, "flight") ||
		strings.Contains(body, "confirmation code") ||
		strings.Contains(body, "check-in:") {
		signals = append(signals, "travel")
	}
	// Tasks / Waiting signals
	if strings.Contains(subj, "action required") ||
		strings.Contains(subj, "deadline") ||
		strings.Contains(body, "action required") ||
		(!isSystemThread(it) && len(it.Messages) > 0 && !it.Messages[len(it.Messages)-1].Mine && strings.Contains(unquotedText(it.Messages[len(it.Messages)-1].Text), "?")) {
		signals = append(signals, "task")
	}
	return signals
}

// Digest renders a Batch as a compact, LLM-ready text digest so an agent can
// read every leased conversation, its IDs, and the element schemas in a single
// tool call without writing inspection scripts.
func Digest(b *Batch) string {
	if len(b.Items) == 0 {
		return NothingToRead(b)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Leased %d conversations for 30 minutes", len(b.Items))
	if b.ID != "" {
		fmt.Fprintf(&sb, " (batch %s)", b.ID)
	}
	sb.WriteString(".\n")
	fmt.Fprintf(&sb, "Submit: %s\n", b.Submit)
	if b.Kind == store.WorkKindDeep {
		sb.WriteString("Deep read: nothing is pre-filled. Read every message in full and answer every element.\n")
	}

	if len(b.Elements) > 0 {
		sb.WriteString("\nElements in this batch:\n")
		writeElementsSummary(&sb, b.Elements)
	}
	if len(b.Topics) > 0 {
		fmt.Fprintf(&sb, "Existing topics: %s\n", strings.Join(b.Topics, ", "))
	}

	type candEntry struct {
		idx     int
		signals []string
		subject string
	}
	var candidates []candEntry
	signalsByIdx := map[int][]string{}
	for i, it := range b.Items {
		if sigs := candidateSignals(it); len(sigs) > 0 {
			signalsByIdx[i] = sigs
			candidates = append(candidates, candEntry{idx: i + 1, signals: sigs, subject: it.Subject})
		}
	}
	if len(candidates) > 0 {
		fmt.Fprintf(&sb, "\nActionable Candidates Index (%d of %d conversations):\n", len(candidates), len(b.Items))
		for _, c := range candidates {
			fmt.Fprintf(&sb, "  - [%d/%d] (%s) %s\n", c.idx, len(b.Items), strings.Join(c.signals, ", "), c.subject)
		}
	} else {
		fmt.Fprintf(&sb, "\nActionable Candidates Index: none detected (all %d appear routine).\n", len(b.Items))
	}

	for i, it := range b.Items {
		fmt.Fprintf(&sb, "\n============================================================\n")
		tag := ""
		if sigs := signalsByIdx[i]; len(sigs) > 0 {
			tag = " [CANDIDATE: " + strings.Join(sigs, ", ") + "]"
		}
		fmt.Fprintf(&sb, "[%d/%d]%s Subject: %s\n", i+1, len(b.Items), tag, it.Subject)
		fmt.Fprintf(&sb, "Thread: %s\n", it.Thread)
		fmt.Fprintf(&sb, "Lease:  %s\n", it.Lease)
		var asks []string
		for _, a := range it.Ask {
			switch a.Unit {
			case elements.UnitMessage:
				asks = append(asks, fmt.Sprintf("%s(msgs:%s)", a.Element, strings.Join(a.Messages, ",")))
			case elements.UnitSender:
				asks = append(asks, fmt.Sprintf("%s(senders:%s)", a.Element, strings.Join(a.Senders, ",")))
			default:
				asks = append(asks, a.Element)
			}
		}
		fmt.Fprintf(&sb, "Ask:    %s\n", strings.Join(asks, ", "))
		if it.Flag != "" {
			fmt.Fprintf(&sb, "Flagged for a careful read: %s\n", it.Flag)
		}
		if len(it.Blank) > 0 {
			fmt.Fprintf(&sb, "Left for you (null in the template, must be answered): %s\n", strings.Join(it.Blank, ", "))
		}
		if len(it.SuggestedLabels) > 0 {
			fmt.Fprintf(&sb, "Suggested labels (a guess from the subject; add to \"labels\" only if right): %s\n",
				strings.Join(it.SuggestedLabels, ", "))
		}
		for _, m := range it.Messages {
			mine := ""
			if m.Mine {
				mine = " (mine)"
			}
			fmt.Fprintf(&sb, "--- Message %s | %s | From: %s%s ---\n",
				m.ID, m.Date.Format(time.RFC3339), m.From, mine)
			sb.WriteString(compactDigestText(m.Text))
			sb.WriteString("\n")
			for _, att := range m.Attachments {
				fmt.Fprintf(&sb, "[Attachment: %s (%s)]\n%s\n", att.Name, att.Type, compactDigestText(att.Text))
			}
		}
	}
	if len(b.Elements) > 0 {
		sb.WriteString("\n============================================================\n")
		sb.WriteString("Schema reminder (valid levels & fields):\n")
		writeElementsSummary(&sb, b.Elements)
	}
	if b.ChecksDue > 0 {
		fmt.Fprintf(&sb, "\nChecks due: %d. After this batch, run `iql work audit next --limit %d` with a different model if you can.\n",
			b.ChecksDue, min(b.ChecksDue, 10))
	}
	return sb.String()
}

// NothingToRead says why a batch is empty, and where more work is.
func NothingToRead(b *Batch) string {
	var sb strings.Builder
	switch {
	case b.Kind == store.WorkKindDeep:
		sb.WriteString("Nothing to read again: no conversation is flagged, and no recent answer is an untouched guess.\n")
	case b.Exhausted:
		fmt.Fprintf(&sb, "Nothing to read among the newest %d conversations. Older ones may still be waiting: "+
			"narrow with --query, e.g. --query \"before:2026-01-01\".\n", scanBudget)
	default:
		sb.WriteString("Nothing to read: every recent conversation is answered.\n")
	}
	if b.ChecksDue > 0 {
		fmt.Fprintf(&sb, "Checks due: %d — run `iql work audit next`.\n", b.ChecksDue)
	}
	return sb.String()
}

var (
	standaloneURL = regexp.MustCompile(`^[\s(<\[|]*https?://\S{50,}[\s)>\]|]*$`)
	longURL       = regexp.MustCompile(`https?://[^\s)>\]"]+`)
	salientLine   = regexp.MustCompile(`(?i)(\$\s*\d|\b\d+\.\d{2}\b|\b(?:usd|eur|gbp|cad)\b|\b(?:total|subtotal|amount paid|balance due|order #|confirmation|check-in|appointment|due date|deadline)\b|\?)`)
)

const (
	digestMaxLines = 45
	digestMaxChars = 2500
)

func compactDigestText(s string) string {
	rawLines := strings.Split(s, "\n")
	var cleanedLines []string
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(cleanedLines) > 0 && cleanedLines[len(cleanedLines)-1] != "" {
				cleanedLines = append(cleanedLines, "")
			}
			continue
		}
		if standaloneURL.MatchString(trimmed) {
			continue
		}
		cleaned := longURL.ReplaceAllStringFunc(trimmed, func(u string) string {
			if len(u) <= 65 {
				return u
			}
			// Keep scheme + host + "…" so quote checking (which splits on "…") stays valid.
			if idx := strings.Index(u, "://"); idx != -1 {
				rest := u[idx+3:]
				if slash := strings.IndexByte(rest, '/'); slash != -1 {
					return u[:idx+3+slash+1] + "…"
				}
			}
			return u[:40] + "…"
		})
		cleanedLines = append(cleanedLines, cleaned)
	}
	for len(cleanedLines) > 0 && cleanedLines[len(cleanedLines)-1] == "" {
		cleanedLines = cleanedLines[:len(cleanedLines)-1]
	}

	var kept []string
	var tailSalient []string
	omitted := 0
	chars := 0
	for _, cleaned := range cleanedLines {
		if len(kept) >= digestMaxLines || chars+len(cleaned) > digestMaxChars {
			if cleaned != "" && salientLine.MatchString(cleaned) && len(tailSalient) < 10 && chars+len(cleaned) <= digestMaxChars+600 {
				tailSalient = append(tailSalient, cleaned)
				chars += len(cleaned) + 1
			} else {
				omitted++
			}
			continue
		}
		kept = append(kept, cleaned)
		chars += len(cleaned) + 1
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	out := strings.Join(kept, "\n")
	if omitted > 0 {
		out += fmt.Sprintf("\n… [%d more lines]", omitted)
	}
	if len(tailSalient) > 0 {
		out += "\n" + strings.Join(tailSalient, "\n")
	}
	return out
}
