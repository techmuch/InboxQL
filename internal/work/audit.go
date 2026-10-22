package work

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
)

// # Checking the loop
//
// `iql work audit next` hands out conversations that were already answered,
// to be read again from scratch — ideally by a different or stronger model,
// with every message in full and no template. The checker never sees the first
// answers: shown them, it would mostly agree, and agreement would prove
// nothing. `iql work audit submit` compares the two.
//
// Agreement is taken as right. A disagreement flags the conversation for a
// deep read and waits for a person, who sees the two answers side by side,
// unlabelled, and says which was right — or neither.
//
// Half of each batch is drawn at random, and only those measure accuracy
// (route.go). The other half is targeted where a wrong answer is likeliest and
// costliest: untouched guesses, low confidence, a conversation that looks like
// it holds a receipt answered "none", senders never checked.

// AuditOptions narrow what `work audit next` hands out.
type AuditOptions struct {
	Limit int
	// Model is the checker's model, recorded with the batch.
	Model string
	// RandomShare is the share drawn at random: the part that measures.
	RandomShare float64
	// Window is how far back answers are checked.
	Window time.Duration
	// Only checks these elements.
	Only []string
}

// auditAsked is what a check lease records: the asks, and — kept from the
// checker — how the conversation was chosen.
type auditAsked struct {
	Asks    []Ask    `json:"asks"`
	Stratum string   `json:"stratum"`
	Reasons []string `json:"reasons,omitempty"`
}

// auditMessages is how much of a conversation a checker reads: more than a
// first read, since a check exists to read carefully.
const auditMessages = 40

// candidate is a conversation that could be checked.
type candidate struct {
	thread  string
	item    *Item
	risk    float64
	reasons []string
}

// AuditNext leases answered conversations to a checker.
func AuditNext(opt AuditOptions) (*Batch, error) {
	if opt.Limit <= 0 {
		opt.Limit = 10
	}
	if opt.RandomShare <= 0 || opt.RandomShare > 1 {
		opt.RandomShare = 0.5
	}
	if opt.Window <= 0 {
		opt.Window = 90 * 24 * time.Hour
	}
	route, err := LoadRouting()
	if err != nil {
		return nil, err
	}
	_, byName, err := elements.Installed()
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-opt.Window)
	b := &Batch{ID: store.NewWorkBatchID(), Kind: store.WorkKindAudit, Elements: map[string]elements.Element{},
		Items: []*Item{}, Submit: "iql --json work audit submit < answers.json — the same shape as work submit"}

	nRandom := int(math.Round(float64(opt.Limit) * opt.RandomShare))
	if nRandom == 0 && opt.Limit >= 2 {
		nRandom = 1
	}
	taken := map[string]bool{}
	lease := func(c *candidate, stratum string) error {
		asked, _ := json.Marshal(auditAsked{Asks: c.item.Ask, Stratum: stratum, Reasons: c.reasons})
		l, err := store.TakeAuditLease(c.thread, string(asked), b.ID, LeaseFor)
		if err != nil {
			return nil // another checker took it
		}
		taken[c.thread] = true
		c.item.Lease, c.item.Expires = l.Token, l.Expires
		for _, a := range c.item.Ask {
			if el, ok := elements.FromAnnotator(byName[a.Element]); ok {
				b.Elements[a.Element] = el
			}
		}
		b.Items = append(b.Items, c.item)
		return nil
	}

	// Random first: these measure, so they are never crowded out.
	keys, err := store.AuditCandidates(since, nRandom*3+5, true)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if len(b.Items) >= nRandom {
			break
		}
		c, err := auditCandidate(k, route, byName, opt.Only)
		if err != nil {
			return nil, err
		}
		if c != nil {
			if err := lease(c, store.StratumRandom); err != nil {
				return nil, err
			}
		}
	}

	// Then the riskiest of the recent answers.
	pool, err := store.AuditCandidates(since, 60, false)
	if err != nil {
		return nil, err
	}
	var cands []*candidate
	for _, k := range pool {
		if taken[k] {
			continue
		}
		c, err := auditCandidate(k, route, byName, opt.Only)
		if err != nil {
			return nil, err
		}
		if c != nil {
			cands = append(cands, c)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].risk > cands[j].risk })
	for _, c := range cands {
		if len(b.Items) >= opt.Limit {
			break
		}
		if err := lease(c, store.StratumTargeted); err != nil {
			return nil, err
		}
	}
	if len(b.Items) > 0 {
		if _, err := store.CreateWorkBatch(b.ID, store.WorkKindAudit, opt.Model, len(b.Items)); err != nil {
			return nil, err
		}
	} else {
		b.ID = ""
	}
	return b, nil
}

// auditCandidate builds the item a checker reads, with what it is asked —
// the elements the loop answered — and how risky the answers look.
func auditCandidate(thread string, route *Routing, byName map[string]*store.Annotator, only []string) (*candidate, error) {
	rows, err := store.AgentAnswersOn(thread)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	msgs, err := store.ThreadReading(thread)
	if err != nil || len(msgs) == 0 {
		return nil, err
	}
	item := &Item{Thread: thread}
	if len(msgs) > auditMessages {
		item.Earlier = len(msgs) - auditMessages
		msgs = msgs[len(msgs)-auditMessages:]
	}
	item.Messages = msgs
	item.Subject = msgs[len(msgs)-1].Subject
	present := map[string]bool{}
	for _, m := range msgs {
		present[m.ID] = true
	}

	asks := map[string]*Ask{}
	var order []string
	seen := map[string]bool{}
	type flags struct{ guessed, unsure, none bool }
	byEl := map[string]*flags{}
	for _, r := range rows {
		a := byName[r.Element]
		if a == nil || !present[r.MessageID] || (len(only) > 0 && !has(only, r.Element)) {
			continue
		}
		el, ok := elements.FromAnnotator(a)
		if !ok {
			continue
		}
		ask := asks[r.Element]
		if ask == nil {
			ask = &Ask{Element: r.Element, Version: a.Version, Unit: el.Unit}
			asks[r.Element] = ask
			order = append(order, r.Element)
			byEl[r.Element] = &flags{}
		}
		key := r.Element + "|" + r.MessageID
		if el.Unit == elements.UnitSender {
			var d map[string]any
			_ = json.Unmarshal([]byte(r.DataJSON), &d)
			addr, _ := d["contact"].(string)
			if addr == "" {
				continue
			}
			key = r.Element + "|" + addr
			if !seen[key] {
				ask.Senders = append(ask.Senders, addr)
			}
		} else if !seen[key] {
			ask.Messages = append(ask.Messages, r.MessageID)
		}
		seen[key] = true
		f := byEl[r.Element]
		f.guessed = f.guessed || r.Prefilled
		f.unsure = f.unsure || (r.Confidence != nil && *r.Confidence < 0.6)
		f.none = f.none || r.Status == store.StatusEmpty
	}
	if len(order) == 0 {
		return nil, nil
	}
	sort.Strings(order)
	for _, name := range order {
		if a := asks[name]; len(a.Messages) > 0 || len(a.Senders) > 0 {
			item.Ask = append(item.Ask, *a)
		}
	}
	if len(item.Ask) == 0 {
		return nil, nil
	}
	if item.People, err = people(msgs); err != nil {
		return nil, err
	}

	// How likely a wrong answer here is, weighted by what it would cost.
	c := &candidate{thread: thread, item: item}
	sigs := candidateSignals(item)
	for _, a := range item.Ask {
		e := route.Elements[a.Element]
		if e == nil {
			e = &ElementRoute{Cost: CostMedium}
		}
		p := (float64(e.Random.Wrong) + 1) / (float64(e.Random.N) + 2)
		mult := 1.0
		f := byEl[a.Element]
		if f.guessed {
			mult *= 2
			c.reasons = append(c.reasons, a.Element+": an untouched guess")
		}
		if f.unsure {
			mult *= 1.5
			c.reasons = append(c.reasons, a.Element+": low confidence")
		}
		if s := signalFor[a.Element]; s != "" && f.none && has(sigs, s) {
			mult *= 3
			c.reasons = append(c.reasons, a.Element+": looks like it holds one, answered none")
		}
		c.risk += costWeight(e.Cost) * p * mult
	}
	if snds := senders(msgs); len(snds) > 0 {
		recs, err := store.SenderRecords(snds[len(snds)-1:])
		if err != nil {
			return nil, err
		}
		rec := recs[snds[len(snds)-1]]
		switch {
		case rec.Streak >= TrustStreak:
			c.risk *= 0.3
		case rec.Checks == 0:
			c.risk *= 1.3
			c.reasons = append(c.reasons, "sender never checked")
		}
	}
	return c, nil
}

// AuditResult says what a check found.
type AuditResult struct {
	Thread   string `json:"thread"`
	Checked  int    `json:"checked"`
	Agreed   int    `json:"agreed"`
	Disagree []struct {
		Element string `json:"element"`
		Target  string `json:"target"`
	} `json:"disagreed,omitempty"`
	// Changed counts answers that changed since the check was taken, so had
	// nothing to compare with.
	Changed  int       `json:"changed,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

// OK reports whether the check was recorded.
func (r *AuditResult) OK() bool { return len(r.Problems) == 0 }

// original is a stored agent answer, as an Answer again.
type original struct {
	answer     Answer
	model      string
	prefilled  bool
	confidence *float64
	messageID  string
}

// originalsOn returns the agent's current answers on a conversation, by
// element and target.
func originalsOn(thread string) (map[string]map[string]*original, error) {
	rows, err := store.AgentAnswersOn(thread)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]*original{}
	for _, r := range rows {
		var d map[string]any
		_ = json.Unmarshal([]byte(r.DataJSON), &d)
		target := r.MessageID
		if c, _ := d["contact"].(string); c != "" {
			target = c
		}
		if out[r.Element] == nil {
			out[r.Element] = map[string]*original{}
		}
		o := out[r.Element][target]
		if o == nil {
			o = &original{model: r.Model, prefilled: r.Prefilled, confidence: r.Confidence, messageID: r.MessageID}
			out[r.Element][target] = o
		}
		switch r.Status {
		case store.StatusEmpty:
			o.answer.None, _ = d["none"].(string)
			if o.answer.None == "" {
				o.answer.None = "none"
			}
		case store.StatusFailed:
			o.answer.Skip = "skipped"
		default:
			if l, _ := d["level"].(string); l != "" {
				o.answer.Level = l
				o.answer.Quote, _ = d["quote"].(string)
			} else if tags, ok := d["tags"].([]any); ok {
				for _, t := range tags {
					if s, ok := t.(string); ok {
						o.answer.Tags = append(o.answer.Tags, s)
					}
				}
			} else {
				o.answer.Records = append(o.answer.Records, d)
			}
		}
		o.answer.Confidence = r.Confidence
	}
	return out, nil
}

// AuditSubmit checks a checker's answers and compares them with the loop's.
func AuditSubmit(sub Submission) (*AuditResult, error) {
	res := &AuditResult{Thread: sub.Thread}
	bad := func(f string, a ...any) { res.Problems = append(res.Problems, Problem{Problem: fmt.Sprintf(f, a...)}) }
	if strings.TrimSpace(sub.Model) == "" {
		bad("model is required: say which model checked, e.g. \"claude-opus-5-5\"")
	}
	lease, err := store.AuditLeaseFor(sub.Thread)
	if err != nil {
		return nil, err
	}
	if lease == nil {
		bad("no check lease on conversation %q: take it with `iql work audit next` first (or it was already checked)", sub.Thread)
		return res, recordAudit(sub, nil, res)
	}
	if lease.Token != sub.Lease {
		bad("the lease does not match: another checker took this conversation")
		return res, recordAudit(sub, lease, res)
	}
	var asked auditAsked
	if err := json.Unmarshal([]byte(lease.Asked), &asked); err != nil {
		return nil, err
	}
	conv, err := readConversation(sub.Thread)
	if err != nil {
		return nil, err
	}
	plan, problems, err := checkAnswers(sub, asked.Asks, conv, nil, false)
	if err != nil {
		return nil, err
	}
	res.Problems = append(res.Problems, problems...)
	if !res.OK() {
		return res, recordAudit(sub, lease, res)
	}

	originals, err := originalsOn(sub.Thread)
	if err != nil {
		return nil, err
	}
	senderOf := map[string]string{}
	for _, m := range conv.msgs {
		senderOf[m.ID], _ = store.NormaliseAddress(m.From)
	}
	var disagreed []string
	for _, p := range plan {
		targets := make([]string, 0, len(p.answers))
		for t := range p.answers {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			checker := p.answers[t]
			o := originals[p.el.Name][t]
			if o == nil {
				res.Changed++
				continue
			}
			origJSON, _ := json.Marshal(o.answer)
			checkJSON, _ := json.Marshal(checker)
			a := &store.WorkAudit{BatchID: lease.BatchID, Thread: sub.Thread, MessageID: o.messageID, Target: t,
				Element: p.el.Name, Stratum: asked.Stratum, Reasons: asked.Reasons,
				Original: string(origJSON), OriginalModel: o.model, OriginalPrefilled: o.prefilled,
				OriginalConfidence: o.confidence, Checker: string(checkJSON), CheckerModel: sub.Model,
				Agree: compareAnswers(p.el, o.answer, checker)}
			if p.el.Unit == elements.UnitSender {
				a.Sender = t
			} else {
				a.Sender = senderOf[o.messageID]
			}
			if err := store.SaveAudit(a); err != nil {
				return nil, err
			}
			if a.Agree == nil {
				continue
			}
			res.Checked++
			if *a.Agree {
				res.Agreed++
				continue
			}
			res.Disagree = append(res.Disagree, struct {
				Element string `json:"element"`
				Target  string `json:"target"`
			}{p.el.Name, t})
			if !has(disagreed, p.el.Name) {
				disagreed = append(disagreed, p.el.Name)
			}
		}
	}
	if len(disagreed) > 0 {
		if err := store.FlagThread(sub.Thread, "a check disagreed on "+strings.Join(disagreed, ", "), store.WorkKindAudit); err != nil {
			return nil, err
		}
	}
	if err := recordAudit(sub, lease, res); err != nil {
		return nil, err
	}
	return res, store.DropAuditLease(sub.Thread)
}

func recordAudit(sub Submission, lease *store.WorkLease, res *AuditResult) error {
	rec := store.WorkSubmissionRecord{Kind: store.WorkKindAudit, Thread: sub.Thread, Model: sub.Model,
		OK: res.OK(), Answers: res.Checked}
	for _, p := range res.Problems {
		rec.Problems = append(rec.Problems, p.String())
	}
	if lease != nil {
		rec.BatchID, rec.LeasedAt = lease.BatchID, lease.CreatedAt
		if err := store.SetBatchModel(lease.BatchID, sub.Model); err != nil {
			return err
		}
		if sub.Usage != nil {
			if err := store.AddBatchUsage(lease.BatchID, *sub.Usage); err != nil {
				return err
			}
		}
	}
	return store.RecordWorkSubmission(rec)
}

// compareAnswers says whether two answers agree, or nil when they cannot be
// compared (either was skipped).
//
// Agreement is about the decision, not the wording: the same level; tags that
// mostly overlap; the same number of records whose numbers, dates and fixed
// choices match. Free text — a merchant's spelling, a task's phrasing — is
// not compared: two careful readers word things differently.
func compareAnswers(el elements.Element, a, b Answer) *bool {
	agree := func(v bool) *bool { return &v }
	if a.Skip != "" || b.Skip != "" {
		return nil
	}
	aNone, bNone := a.None != "", b.None != ""
	if aNone || bNone {
		return agree(aNone == bNone)
	}
	switch el.Shape {
	case elements.ShapeLevels:
		return agree(strings.EqualFold(a.Level, b.Level))
	case elements.ShapeTags:
		return agree(tagsAgree(a.Tags, b.Tags))
	}
	if len(a.Records) != len(b.Records) {
		return agree(false)
	}
	used := make([]bool, len(b.Records))
	for _, ra := range a.Records {
		found := false
		for j, rb := range b.Records {
			if !used[j] && recordsAgree(el.Fields, ra, rb) {
				used[j], found = true, true
				break
			}
		}
		if !found {
			return agree(false)
		}
	}
	return agree(true)
}

func tagsAgree(a, b []string) bool {
	x, y := normTags(a), normTags(b)
	if len(x) == 0 || len(y) == 0 {
		return len(x) == len(y)
	}
	set := map[string]bool{}
	for _, t := range x {
		set[t] = true
	}
	shared := 0
	for _, t := range y {
		if set[t] {
			shared++
		}
	}
	smaller := min(len(x), len(y))
	return shared*2 >= smaller
}

func recordsAgree(fields []elements.Field, a, b map[string]any) bool {
	for _, f := range fields {
		va, vb := a[f.Name], b[f.Name]
		if empty(va) || empty(vb) {
			continue // one reader left it out: not a disagreement about it
		}
		switch {
		case f.Type == elements.TypeNumber:
			x, y := toNumber(va), toNumber(vb)
			if math.Abs(x-y) > math.Max(0.01, 0.01*math.Max(math.Abs(x), math.Abs(y))) {
				return false
			}
		case f.Type == elements.TypeDate || f.Type == elements.TypeDateTime:
			x, y := fmt.Sprint(va), fmt.Sprint(vb)
			if len(x) >= 10 && len(y) >= 10 && x[:10] != y[:10] {
				return false
			}
		case len(f.Enum) > 0:
			if !strings.EqualFold(fmt.Sprint(va), fmt.Sprint(vb)) {
				return false
			}
		}
	}
	return true
}

// Resolve records which of two disagreeing answers a person said was right.
//
// The right answer becomes a ruling — it outranks either model from then on —
// and when it was the checker's, what that answer does follows too: a task
// the first read missed is proposed, an event put on the calendar as a
// proposal. "Neither" records nothing and leaves the conversation flagged for
// a deep read.
func Resolve(id, choice string, cal *calsync.Engine) (*store.WorkAudit, error) {
	a, err := store.GetAudit(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no check %q", id)
	}
	if a.Agree == nil || *a.Agree {
		return nil, fmt.Errorf("check %s did not disagree: there is nothing to settle", id)
	}
	if a.Resolution != "" {
		return nil, fmt.Errorf("check %s was already settled: %s", id, a.Resolution)
	}
	if err := store.ResolveAudit(id, choice); err != nil {
		return nil, err
	}
	_, byName, err := elements.Installed()
	if err != nil {
		return nil, err
	}
	ann := byName[a.Element]
	if ann != nil && choice != store.ResolvedNeither {
		el, _ := elements.FromAnnotator(ann)
		raw, model := a.Original, a.OriginalModel
		if choice == store.ResolvedChecker {
			raw, model = a.Checker, a.CheckerModel
		}
		var ans Answer
		if err := json.Unmarshal([]byte(raw), &ans); err != nil {
			return nil, err
		}
		anns := annotations(el, ans, "", a.Target)
		if err := store.RecordElementRuling(ann.ID, ann.Version, a.MessageID, anns, store.RuledAudit); err != nil {
			return nil, err
		}
		if choice == store.ResolvedChecker && ans.Skip == "" && ans.None == "" {
			if err := applyAnswer(el, ans, anns, a.Thread, a.MessageID, a.Target, model, false,
				store.ContactFromHuman, store.SourceHuman, cal, &Result{}); err != nil {
				return nil, err
			}
		}
	}
	if choice != store.ResolvedNeither {
		open, err := store.OpenDisagreementsOn(a.Thread)
		if err != nil {
			return nil, err
		}
		if f, err := store.ThreadFlag(a.Thread); err == nil && f != nil && open == 0 && f.Source == store.WorkKindAudit {
			if err := store.UnflagThread(a.Thread); err != nil {
				return nil, err
			}
		}
	}
	return store.GetAudit(id)
}

// Describe renders an answer for a person: what it says, in a few words.
func Describe(raw string) string {
	var a Answer
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return raw
	}
	switch {
	case a.Skip != "":
		return "could not tell"
	case a.None != "":
		return "nothing here"
	case a.Level != "":
		return a.Level
	case len(a.Tags) > 0:
		return strings.Join(a.Tags, ", ")
	}
	var parts []string
	for _, r := range a.Records {
		var bits []string
		for _, k := range []string{"title", "what", "merchant", "provider", "kind", "start", "due", "date", "total", "owner"} {
			if v, ok := r[k]; ok && !empty(v) {
				s := fmt.Sprint(v)
				if f, ok := v.(float64); ok {
					s = strconv.FormatFloat(f, 'f', -1, 64)
				}
				bits = append(bits, s)
			}
		}
		parts = append(parts, strings.Join(bits, " · "))
	}
	return strings.Join(parts, "; ")
}
