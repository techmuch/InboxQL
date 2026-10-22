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
				c, err := store.GetContactSummary(s)
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
// thread keys, lease tokens, message IDs, sender addresses, default quotes from
// the conversation subject/text, and default "none" entries pre-filled so a
// harness only edits the fields that apply.
func Template(b *Batch, model string) []Submission {
	if strings.TrimSpace(model) == "" {
		model = "agent"
	}
	senderCache := map[string]store.SenderHistory{}
	lookupSender := func(it *Item) store.SenderHistory {
		snds := senders(it.Messages)
		if len(snds) == 0 {
			return store.SenderHistory{}
		}
		addr := snds[len(snds)-1]
		if h, ok := senderCache[addr]; ok {
			return h
		}
		h, _ := store.SenderDefaults(addr)
		senderCache[addr] = h
		return h
	}

	out := make([]Submission, 0, len(b.Items))
	for _, it := range b.Items {
		defQuote := defaultQuote(it.Messages)
		allSystem := isSystemThread(it)
		hist := lookupSender(it)
		ans := map[string]json.RawMessage{}
		for _, a := range it.Ask {
			el := b.Elements[a.Element]
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
					m[s] = Answer{Level: guessSenderLevel(s)}
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
					lvl := ""
					switch {
					case el.HasLevel("no-one"):
						lvl = "no-one"
					case hist.Importance != "" && el.HasLevel(hist.Importance):
						lvl = hist.Importance
					case allSystem && el.HasLevel("low"):
						lvl = "low"
					case el.HasLevel("normal"):
						lvl = "normal"
					case len(el.Levels) > 0:
						lvl = el.Levels[0].Label
					}
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
		if lbls == nil {
			lbls = []string{}
		}
		out = append(out, Submission{
			Thread:  it.Thread,
			Lease:   it.Lease,
			Model:   model,
			Labels:  lbls,
			Answers: ans,
		})
	}
	return out
}

func inferTopicsAndLabels(it *Item) (topics []string, labels []string) {
	subj := strings.ToLower(it.Subject)
	var addr string
	if snds := senders(it.Messages); len(snds) > 0 {
		addr = strings.ToLower(snds[len(snds)-1])
	}
	switch {
	case strings.Contains(addr, "facebookmail.com") || strings.Contains(subj, "facebook") || strings.Contains(subj, "reel"):
		return []string{"facebook", "social"}, []string{"social", "facebook"}
	case strings.Contains(addr, "navyfederal.org"):
		return []string{"navy-federal", "banking"}, []string{"finance"}
	case strings.Contains(addr, "equifax") || strings.Contains(addr, "transunion") || strings.Contains(addr, "experian") || strings.Contains(subj, "credit report") || strings.Contains(subj, "credit alert"):
		return []string{"finance", "credit"}, []string{"finance"}
	case strings.Contains(addr, "churchofjesuschrist.org") || strings.Contains(subj, "ward conference") || strings.Contains(subj, "sacrament") || strings.Contains(subj, "tithing"):
		return []string{"church"}, []string{"church"}
	case strings.Contains(addr, "phreesia") || strings.Contains(addr, "waystar") || strings.Contains(subj, "northside hospital") || strings.Contains(subj, "center pointe"):
		return []string{"health"}, []string{"health"}
	case strings.Contains(addr, "amazon.com") && (strings.HasPrefix(subj, "ordered") || strings.HasPrefix(subj, "shipped") || strings.HasPrefix(subj, "delivered")):
		return []string{"amazon", "shopping"}, []string{"shopping"}
	case strings.Contains(subj, "receipt") || strings.Contains(subj, "order #") || strings.Contains(subj, "invoice"):
		return []string{"receipt"}, []string{"receipts"}
	case strings.Contains(subj, "security alert") || strings.Contains(subj, "verification code") || strings.Contains(subj, "sign-in"):
		return []string{"security"}, []string{"security"}
	case strings.Contains(addr, "peacock") || strings.Contains(addr, "audible") || strings.Contains(addr, "vrbo") || strings.Contains(subj, "discount") || strings.Contains(subj, "sale") || strings.Contains(subj, "save "):
		return []string{"promotions"}, []string{"promotions"}
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
	return ""
}

func isSystemThread(it *Item) bool {
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
			k = guessSenderLevel(s)
		}
		if k != "system" {
			return false
		}
	}
	return true
}

func guessSenderLevel(addr string) string {
	a := strings.ToLower(strings.TrimSpace(addr))
	for _, kw := range []string{
		"noreply", "no-reply", "do-not-reply", "donotreply",
		"mailer-daemon", "postmaster", "notifications@", "notification@",
		"alerts@", "alert@", "receipts@", "receipt@", "notice@",
		"updates@", "friendupdates@", "order-update@", "shipment-tracking@",
		"auto-confirm@", "newsletter@", "news@", "digest@", "billing@",
		"info@", "service@", "automated", "bounce",
		"messenger@messaging.squareup.com",
		"@facebookmail.com", "@email.amazonses.com", "@us-west-2.amazonses.com",
		"@notifications.google.com",
	} {
		if strings.Contains(a, kw) {
			return "system"
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
	for _, m := range it.Messages {
		bodyBuilder.WriteString(strings.ToLower(m.Text))
		bodyBuilder.WriteByte('\n')
	}
	body := bodyBuilder.String()

	var signals []string
	// Receipts signals
	if strings.Contains(subj, "receipt") ||
		strings.HasPrefix(subj, "ordered ") ||
		strings.Contains(subj, "order #") ||
		strings.Contains(body, "grand total:") ||
		strings.Contains(body, "amount paid") ||
		strings.Contains(body, "you paid $") ||
		(strings.Contains(body, "total:") && strings.Contains(body, "order number:")) {
		signals = append(signals, "receipt")
	}
	// Events signals
	if strings.Contains(subj, "appointment") ||
		strings.Contains(subj, "agenda") ||
		strings.Contains(subj, "tailgate") ||
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
	// Tasks signals
	if strings.Contains(subj, "action required") ||
		strings.Contains(subj, "tithing declaration") ||
		strings.Contains(subj, "deadline") ||
		strings.Contains(body, "action required") {
		signals = append(signals, "task")
	}
	return signals
}

// Digest renders a Batch as a compact, LLM-ready text digest so an agent can
// read every leased conversation, its IDs, and the element schemas in a single
// tool call without writing inspection scripts.
func Digest(b *Batch) string {
	if len(b.Items) == 0 {
		return "Nothing to read: every recent conversation is answered.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Leased %d conversations for 30 minutes.\n", len(b.Items))
	fmt.Fprintf(&sb, "Submit: %s\n", b.Submit)

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
	return sb.String()
}

var (
	standaloneURL = regexp.MustCompile(`^[\s(<\[|]*https?://\S{50,}[\s)>\]|]*$`)
	longURL       = regexp.MustCompile(`https?://[^\s)>\]"]+`)
)

const (
	digestMaxLines = 45
	digestMaxChars = 2500
)

func compactDigestText(s string) string {
	rawLines := strings.Split(s, "\n")
	var kept []string
	omitted := 0
	chars := 0
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(kept) > 0 && kept[len(kept)-1] != "" {
				kept = append(kept, "")
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
		if len(kept) >= digestMaxLines || chars+len(cleaned) > digestMaxChars {
			omitted++
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
	return out
}
