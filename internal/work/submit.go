package work

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
)

// Submission is the answer for one conversation.
type Submission struct {
	Thread string `json:"thread"`
	Lease  string `json:"lease"`
	// Model is who answered — "claude-opus-5-5" — kept with every answer, so
	// a run can be scored and, if it was wrong, undone.
	Model string `json:"model"`
	// Labels optionally adds mail labels (with origin=agent) to the messages
	// in this conversation as part of the same submission.
	Labels []string `json:"labels,omitempty"`
	// Archive optionally archives the conversation (removes the inbox label
	// with origin=agent).
	Archive bool `json:"archive,omitempty"`
	// Answers holds one entry per element asked. A conversation element's
	// entry is an Answer; a message element's maps message id to Answer; a
	// sender element's maps address to Answer.
	Answers map[string]json.RawMessage `json:"answers"`
}

// Answer is what was concluded about one thing.
type Answer struct {
	// Exactly one of these.
	Level   string           `json:"level,omitempty"`
	Records []map[string]any `json:"records,omitempty"`
	Tags    []string         `json:"tags,omitempty"`
	// None says the element does not apply here, and why.
	None string `json:"none,omitempty"`
	// Skip says it could not be answered, and why. It is recorded, so the
	// conversation is not offered again until the element changes.
	Skip string `json:"skip,omitempty"`

	Confidence *float64 `json:"confidence,omitempty"`
	Why        string   `json:"why,omitempty"`
	// Quote is words from the conversation that show it, checked.
	Quote string `json:"quote,omitempty"`
}

// Problem is one thing wrong with a submission.
type Problem struct {
	Element string `json:"element,omitempty"`
	Target  string `json:"target,omitempty"`
	Problem string `json:"problem"`
}

// Result says what a submission did.
type Result struct {
	Thread   string         `json:"thread"`
	Stored   map[string]int `json:"stored,omitempty"`
	Tickets  int            `json:"ticketsProposed,omitempty"`
	Events   int            `json:"eventsProposed,omitempty"`
	Contacts int            `json:"contactsClassified,omitempty"`
	Labels   int            `json:"labelsAdded,omitempty"`
	Archived int            `json:"archived,omitempty"`
	Problems []Problem      `json:"problems,omitempty"`
}

// OK reports whether the submission was stored.
func (r *Result) OK() bool { return len(r.Problems) == 0 }

// Submit checks a conversation's answers and, if every one is sound, stores
// them all. One problem stores nothing: half a conversation's answers would
// leave it neither pending nor done, and the harness can fix and resend.
func Submit(sub Submission, cal *calsync.Engine) (*Result, error) {
	res := &Result{Thread: sub.Thread, Stored: map[string]int{}}
	bad := func(el, target, f string, a ...any) {
		res.Problems = append(res.Problems, Problem{Element: el, Target: target, Problem: fmt.Sprintf(f, a...)})
	}
	if strings.TrimSpace(sub.Model) == "" {
		bad("", "", "model is required: say which model answered, e.g. \"claude-opus-5-5\"")
	}

	lease, err := store.LeaseFor(sub.Thread)
	if err != nil {
		return nil, err
	}
	if lease == nil {
		bad("", "", "no lease on conversation %q: take it with `iql work next` first (or it was already submitted)", sub.Thread)
		return res, nil
	}
	if lease.Token != sub.Lease {
		bad("", "", "the lease does not match: another run took this conversation; take it again with `iql work next`")
		return res, nil
	}
	var asked []Ask
	if err := json.Unmarshal([]byte(lease.Asked), &asked); err != nil {
		return nil, err
	}

	msgs, err := store.ThreadReading(sub.Thread)
	if err != nil {
		return nil, err
	}
	corpus := corpusOf(msgs)
	fromOf := map[string]string{} // sender → their newest message
	for _, m := range msgs {
		if a, _ := store.NormaliseAddress(m.From); a != "" {
			fromOf[a] = m.ID
		}
	}

	_, byName, err := elements.Installed()
	if err != nil {
		return nil, err
	}

	// Check everything first.
	type planned struct {
		ask     Ask
		el      elements.Element
		ann     *store.Annotator
		answers map[string]Answer // target → answer
	}
	var plan []planned
	askedNames := map[string]bool{}
	for _, ask := range asked {
		askedNames[ask.Element] = true
		a := byName[ask.Element]
		if a == nil {
			bad(ask.Element, "", "this element was switched off or removed since it was asked")
			continue
		}
		el, _ := elements.FromAnnotator(a)
		if a.Version != ask.Version {
			bad(ask.Element, "", "its instructions changed since this conversation was taken (version %d, now %d): take it again", ask.Version, a.Version)
			continue
		}
		raw, ok := sub.Answers[ask.Element]
		if !ok {
			bad(ask.Element, "", "not answered: answer it, or give none or skip with a reason")
			continue
		}
		answers := map[string]Answer{}
		targets := ask.Messages
		if el.Unit == elements.UnitSender {
			targets = ask.Senders
		}
		if el.Unit == elements.UnitThread {
			var ans Answer
			if err := strictDecode(raw, &ans); err != nil {
				bad(ask.Element, "", "not an answer: %v", err)
				continue
			}
			answers[targets[0]] = ans
		} else {
			var m map[string]Answer
			if err := strictDecode(raw, &m); err != nil {
				bad(ask.Element, "", "should map each %s to an answer: %v", targetNoun(el.Unit), err)
				continue
			}
			for k, v := range m {
				if el.Unit == elements.UnitSender {
					k = strings.ToLower(strings.TrimSpace(k))
				}
				answers[k] = v
			}
			for k := range answers {
				if !has(targets, k) {
					bad(ask.Element, k, "this %s was not asked about", targetNoun(el.Unit))
				}
			}
			for _, t := range targets {
				if _, ok := answers[t]; !ok {
					bad(ask.Element, t, "not answered")
				}
			}
		}
		for t, ans := range answers {
			for _, p := range check(el, ans, corpus) {
				bad(ask.Element, t, "%s", p)
			}
		}
		plan = append(plan, planned{ask: ask, el: el, ann: a, answers: answers})
	}
	for name := range sub.Answers {
		if !askedNames[name] {
			bad(name, "", "not asked about this conversation; answers are taken only for what `work next` asked")
		}
	}
	var cleanLabels []string
	for _, l := range sub.Labels {
		n := store.NormalizeLabel(l)
		if n == "" {
			continue
		}
		if n == store.LabelTrash || n == store.LabelSpam {
			bad("labels", l, "trash and spam are not added through work submit; use `iql mail trash` or `iql mail spam` when asked")
			continue
		}
		cleanLabels = append(cleanLabels, n)
	}
	if !res.OK() {
		return res, nil
	}

	// Then store.
	for _, p := range plan {
		targets := make([]string, 0, len(p.answers))
		for t := range p.answers {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			ans := p.answers[t]
			subject := t
			if p.el.Unit == elements.UnitSender {
				subject = fromOf[t]
			}
			anns := annotations(p.el, ans, sub.Model, t)
			if err := store.SaveAnnotations(p.ann.ID, p.ann.Version, subject, anns); err != nil {
				return nil, fmt.Errorf("%s: %w", p.el.Name, err)
			}
			res.Stored[p.el.Name] += len(anns)
			if ans.Skip != "" || ans.None != "" {
				continue
			}
			switch p.el.Target {
			case elements.TargetContacts:
				if err := store.SetContactKind(t, ans.Level, store.ContactFromAgent); err != nil {
					return nil, err
				}
				res.Contacts++
			case elements.TargetTopics:
				if err := store.SetMessageTopics(subject, normTags(ans.Tags), store.SourceAgent, ans.Confidence); err != nil {
					return nil, err
				}
			case elements.TargetTickets:
				n, err := proposeTickets(sub.Thread, subject, ans, anns)
				if err != nil {
					return nil, err
				}
				res.Tickets += n
			case elements.TargetCalendar:
				n, err := proposeEvents(cal, sub.Thread, subject, ans)
				if err != nil {
					return nil, err
				}
				res.Events += n
			}
		}
	}
	if len(cleanLabels) > 0 || sub.Archive {
		msgIDs := make([]string, len(msgs))
		for i, m := range msgs {
			msgIDs[i] = m.ID
		}
		if len(cleanLabels) > 0 {
			n, err := store.ChangeLabels(msgIDs, store.LabelChange{Add: cleanLabels, Source: store.LabelSourceAgent})
			if err != nil {
				return nil, err
			}
			res.Labels = n
		}
		if sub.Archive {
			ch := store.Moves["archive"]
			ch.Source = store.LabelSourceAgent
			n, err := store.ChangeLabels(msgIDs, ch)
			if err != nil {
				return nil, err
			}
			res.Archived = n
		}
	}
	return res, store.DropLease(sub.Thread)
}

func targetNoun(unit string) string {
	if unit == elements.UnitSender {
		return "sender"
	}
	return "message"
}

func strictDecode(raw json.RawMessage, v any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// check returns what is wrong with one answer.
func check(el elements.Element, a Answer, corpus string) []string {
	var out []string
	given := 0
	for _, set := range []bool{a.Level != "", len(a.Records) > 0, len(a.Tags) > 0, a.None != "", a.Skip != ""} {
		if set {
			given++
		}
	}
	if given != 1 {
		return []string{shapeHint(el)}
	}
	if a.Confidence != nil && (*a.Confidence < 0 || *a.Confidence > 1 || math.IsNaN(*a.Confidence)) {
		out = append(out, "confidence is between 0 and 1")
	}
	if a.Skip != "" || a.None != "" {
		if a.None != "" && el.Shape == elements.ShapeLevels {
			out = append(out, "a levelled element has no none: choose a level, or skip with a reason")
		}
		return out
	}
	switch el.Shape {
	case elements.ShapeLevels:
		if a.Level == "" || len(a.Records) > 0 || len(a.Tags) > 0 {
			return []string{shapeHint(el)}
		}
		if !el.HasLevel(a.Level) {
			out = append(out, fmt.Sprintf("%q is not a level; the levels are %s", a.Level, levelNames(el)))
		}
		// A sender's kind is judged from how they write, not one sentence.
		if el.Unit != elements.UnitSender {
			if p := quoted(a.Quote, corpus); p != "" {
				out = append(out, p)
			}
		}
	case elements.ShapeRecords:
		if len(a.Records) == 0 {
			return []string{shapeHint(el)}
		}
		for i, r := range a.Records {
			q, _ := r["quote"].(string)
			if p := quoted(q, corpus); p != "" {
				out = append(out, fmt.Sprintf("record %d: %s", i+1, p))
			}
			for _, p := range checkRecord(el.Fields, r, true) {
				out = append(out, fmt.Sprintf("record %d: %s", i+1, p))
			}
		}
	case elements.ShapeTags:
		if len(a.Tags) == 0 {
			return []string{shapeHint(el)}
		}
		if n := len(normTags(a.Tags)); n == 0 || n > 8 {
			out = append(out, "give one to eight tags")
		}
	}
	return out
}

func shapeHint(el elements.Element) string {
	switch el.Shape {
	case elements.ShapeLevels:
		return "answer with exactly one of: level (one of " + levelNames(el) + "), or skip"
	case elements.ShapeRecords:
		return "answer with exactly one of: records (a list), none, or skip"
	default:
		return "answer with exactly one of: tags (a list), none, or skip"
	}
}

func levelNames(el elements.Element) string {
	names := make([]string, len(el.Levels))
	for i, l := range el.Levels {
		names[i] = l.Label
	}
	return strings.Join(names, ", ")
}

// checkRecord checks a record against its fields. top allows the quote and
// why every record carries.
func checkRecord(fields []elements.Field, r map[string]any, top bool) []string {
	var out []string
	known := map[string]elements.Field{}
	for _, f := range fields {
		known[f.Name] = f
	}
	for k, v := range r {
		if top && (k == "quote" || k == "why") {
			if _, ok := v.(string); !ok {
				out = append(out, k+" is text")
			}
			continue
		}
		f, ok := known[k]
		if !ok {
			out = append(out, fmt.Sprintf("%q is not a field (fields: %s)", k, fieldNames(fields)))
			continue
		}
		if p := checkValue(f, v); p != "" {
			out = append(out, k+": "+p)
		} else if f.Type == elements.TypeNumber {
			r[k] = toNumber(v)
		}
	}
	for _, f := range fields {
		if f.Required && empty(r[f.Name]) {
			out = append(out, f.Name+" is required")
		}
	}
	return out
}

func fieldNames(fields []elements.Field) string {
	n := make([]string, len(fields))
	for i, f := range fields {
		n[i] = f.Name
	}
	return strings.Join(n, ", ")
}

func empty(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

func checkValue(f elements.Field, v any) string {
	if v == nil {
		return ""
	}
	switch f.Type {
	case elements.TypeText:
		s, ok := v.(string)
		if !ok {
			return "is text"
		}
		if len(f.Enum) > 0 && !has(f.Enum, s) {
			return fmt.Sprintf("%q is not one of %s", s, strings.Join(f.Enum, ", "))
		}
	case elements.TypeNumber:
		if math.IsNaN(toNumber(v)) {
			return "is a number, e.g. 159.00"
		}
	case elements.TypeBool:
		if _, ok := v.(bool); !ok {
			return "is true or false"
		}
	case elements.TypeDate:
		s, ok := v.(string)
		if !ok {
			return "is a date, e.g. 2026-10-20"
		}
		if _, err := parseWhen(s); err != nil {
			return "is a date, e.g. 2026-10-20"
		}
	case elements.TypeDateTime:
		s, ok := v.(string)
		if !ok {
			return "is a date and time, e.g. 2026-10-20T14:30"
		}
		if _, err := parseWhen(s); err != nil {
			return "is a date and time, e.g. 2026-10-20T14:30"
		}
	case elements.TypeList:
		list, ok := v.([]any)
		if !ok {
			return "is a list"
		}
		for i, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return fmt.Sprintf("item %d is not an object", i+1)
			}
			if p := checkRecord(f.Of, m, false); len(p) > 0 {
				return fmt.Sprintf("item %d: %s", i+1, strings.Join(p, "; "))
			}
		}
	}
	return ""
}

// toNumber reads a number, or a string holding one ("$1,299.00"), or NaN.
func toNumber(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		s := strings.NewReplacer("$", "", "£", "", "€", "", ",", "", " ", "").Replace(x)
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n
		}
	}
	return math.NaN()
}

var whenLayouts = []string{
	time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02",
}

// parseWhen reads a date or a date and time. A time without a zone is local.
func parseWhen(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, l := range whenLayouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unreadable time %q", s)
}

// Quotes.

// corpusOf is a conversation's text, normalised the way quotes are, for
// checking that a quote is really in it.
func corpusOf(msgs []*store.ReadingMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Subject + "\n" + m.Text + "\n")
		for _, f := range m.Attachments {
			b.WriteString(f.Name + "\n" + f.Text + "\n")
		}
	}
	return normQuote(b.String())
}

var quoteFold = strings.NewReplacer("‘", "'", "’", "'", "“", "\"", "”", "\"",
	"–", "-", "—", "-", " ", " ",
	"\u200b", "", "\u200c", "", "\u200d", "", "\u2060", "", "\ufeff", "", "\u034f", "", "\u00ad", "")

func normQuote(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(quoteFold.Replace(s))), " ")
}

// quoted says what is wrong with a quote, or "" when it is in the corpus.
// "…" or "..." may join pieces; each must be there.
func quoted(q, corpus string) string {
	if strings.TrimSpace(q) == "" {
		return "quote is required: copy words from the conversation that show this"
	}
	parts := strings.FieldsFunc(strings.ReplaceAll(q, "...", "…"), func(r rune) bool { return r == '…' })
	found := 0
	for _, p := range parts {
		p = normQuote(p)
		if len(p) < 3 {
			continue
		}
		if !strings.Contains(corpus, p) {
			return fmt.Sprintf("quote %q is not in the conversation: copy it exactly", strings.TrimSpace(q))
		}
		found++
	}
	if found == 0 {
		return "quote is too short to check"
	}
	return ""
}

// Storing.

func annotations(el elements.Element, a Answer, model, target string) []*store.Annotation {
	base := func(status string, data map[string]any) *store.Annotation {
		if a.Why != "" {
			data["why"] = a.Why
		}
		if a.Quote != "" {
			data["quote"] = a.Quote
		}
		b, _ := json.Marshal(data)
		return &store.Annotation{Status: status, Source: store.SourceAgent, Model: model,
			Confidence: a.Confidence, DataJSON: string(b)}
	}
	switch {
	case a.Skip != "":
		ann := base(store.StatusFailed, map[string]any{})
		ann.Error = "skipped: " + a.Skip
		return []*store.Annotation{ann}
	case a.None != "":
		return []*store.Annotation{base(store.StatusEmpty, map[string]any{"none": a.None})}
	case a.Level != "":
		data := map[string]any{"level": a.Level}
		if el.Unit == elements.UnitSender {
			data["contact"] = target
		}
		return []*store.Annotation{base(store.StatusOK, data)}
	case len(a.Tags) > 0:
		return []*store.Annotation{base(store.StatusOK, map[string]any{"tags": normTags(a.Tags)})}
	}
	out := make([]*store.Annotation, 0, len(a.Records))
	for i, r := range a.Records {
		b, _ := json.Marshal(r)
		out = append(out, &store.Annotation{Seq: i, Status: store.StatusOK, Source: store.SourceAgent,
			Model: model, Confidence: a.Confidence, DataJSON: string(b)})
	}
	return out
}

func normTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func str(r map[string]any, k string) string {
	s, _ := r[k].(string)
	return strings.TrimSpace(s)
}

func proposeTickets(thread, messageID string, a Answer, anns []*store.Annotation) (int, error) {
	n := 0
	for i, r := range a.Records {
		title := str(r, "what")
		dup, err := store.AgentTicketInThread(thread, title)
		if err != nil {
			return n, err
		}
		if dup {
			continue
		}
		body := "Owed by: " + str(r, "owner")
		if q := str(r, "quote"); q != "" {
			body += "\n\n> " + q
		}
		if w := str(r, "why"); w != "" {
			body += "\n\n" + w
		}
		t := &store.Ticket{ThreadKey: thread, Title: title, Body: body, Status: store.TicketProposed,
			Origin: store.OriginAgentTicket, Confidence: a.Confidence}
		if d := str(r, "due"); d != "" {
			if due, err := parseWhen(d); err == nil {
				t.DueAt = &due
			}
		}
		if err := store.SaveTicket(t); err != nil {
			return n, err
		}
		if err := store.AttachSource(t.ID, messageID, anns[i].ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func proposeEvents(cal *calsync.Engine, thread, messageID string, a Answer) (int, error) {
	if cal == nil {
		cal = calsync.New()
	}
	if err := store.EnsureDefaultCalendars(); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range a.Records {
		start, err := parseWhen(str(r, "start"))
		if err != nil {
			continue
		}
		ev := calendar.Event{Summary: str(r, "title"), Location: str(r, "location"), Start: start}
		allDay, _ := r["all_day"].(bool)
		if len(str(r, "start")) == len("2006-01-02") {
			allDay = true
		}
		ev.AllDay = allDay
		switch end, err := parseWhen(str(r, "end")); {
		case err == nil && end.After(start):
			ev.End = end
		case allDay:
			ev.End = start.AddDate(0, 0, 1)
		default:
			ev.End = start.Add(time.Hour)
		}
		notes := str(r, "notes")
		if q := str(r, "quote"); q != "" {
			notes = strings.TrimSpace(notes + "\n\nFrom the email: “" + q + "”")
		}
		ev.Description = notes
		dup, err := store.EventFromMessage(messageID, ev.Summary, ev.Start)
		if err != nil {
			return n, err
		}
		if dup {
			continue
		}
		if _, err := cal.CreateFrom(store.LocalCalendarID, ev, calsync.OriginAgent, messageID, thread); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
