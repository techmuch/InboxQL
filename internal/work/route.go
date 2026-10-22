package work

import (
	"fmt"
	"math"
	"time"

	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
)

// # Spending effort where a wrong answer is likely and costly
//
// Each element has a target — how often its answers must be right — and a
// cost — what a wrong one does. A missed receipt or task is expensive; a
// slightly wrong topic is not. Random checks (`iql work audit`) measure how
// often each element's answers are right, and the measurement decides two
// things:
//
//   - whether the template may guess the element at all. An element whose
//     checked answers miss the target is left blank, so it has to be read —
//     unless the sender has a long record of being answered right, which is
//     the cheap tier earned per sender;
//   - how often to check. Unmeasured or failing elements are checked often;
//     ones comfortably meeting their target, rarely.
//
// The estimate uses random checks only. Targeted checks are chosen because an
// answer looked likely to be wrong, so counting them would overstate how often
// the loop is wrong.

// Costs of a wrong answer.
const (
	CostHigh   = "high"
	CostMedium = "medium"
	CostLow    = "low"
)

// DefaultTargets are what each catalog element must reach until changed.
var DefaultTargets = map[string]store.WorkTarget{
	"receipts":   {Element: "receipts", Target: 0.97, Cost: CostHigh},
	"travel":     {Element: "travel", Target: 0.97, Cost: CostHigh},
	"events":     {Element: "events", Target: 0.95, Cost: CostHigh},
	"tasks":      {Element: "tasks", Target: 0.90, Cost: CostHigh},
	"waiting":    {Element: "waiting", Target: 0.90, Cost: CostMedium},
	"importance": {Element: "importance", Target: 0.85, Cost: CostLow},
	"sender":     {Element: "sender", Target: 0.90, Cost: CostLow},
	"topics":     {Element: "topics", Target: 0.80, Cost: CostLow},
}

var fallbackTarget = store.WorkTarget{Target: 0.90, Cost: CostMedium}

// MinChecks is the fewest settled random checks an estimate is acted on.
const MinChecks = 10

// TrustStreak is how many checks in a row a sender's answers must pass to be
// guessed even where their element is missing its target.
const TrustStreak = 10

// routeWindow is how far back checks count: old enough to have enough, recent
// enough that a better model or better instructions show.
const routeWindow = 180 * 24 * time.Hour

// Route statuses.
const (
	StatusUnmeasured = "unmeasured"
	StatusMeeting    = "meeting"
	StatusUncertain  = "uncertain"
	StatusMissing    = "missing"
)

// Tally is a count of settled checks.
type Tally struct {
	N     int `json:"n"`
	Right int `json:"right"`
	Wrong int `json:"wrong"`
	// Open counts disagreements nobody has settled: neither right nor wrong yet.
	Open int `json:"open"`
}

func (t *Tally) add(o store.AuditOutcome) {
	if !o.Known {
		t.Open++
		return
	}
	t.N++
	if o.Right {
		t.Right++
	} else {
		t.Wrong++
	}
}

// Estimate is an accuracy and the range it is probably in.
type Estimate struct {
	Accuracy float64 `json:"accuracy"`
	Low      float64 `json:"low"`
	High     float64 `json:"high"`
	N        int     `json:"n"`
	// Open counts disagreements nobody has settled. They are where mistakes
	// are, so leaving them out would flatter the loop: the range assumes
	// every one was the loop's mistake at its low end, and none at its high.
	Open int `json:"open,omitempty"`
}

// estimate is the Wilson interval at 95%, which stays inside 0..1 and is
// honest about small samples where the plain ± is not, widened by whatever
// is still unsettled.
func estimate(t Tally) *Estimate {
	if t.N == 0 {
		return nil
	}
	all := t.N + t.Open
	lo, _ := wilson(t.Right, all)
	_, hi := wilson(t.Right+t.Open, all)
	return &Estimate{Accuracy: float64(t.Right) / float64(t.N), Low: lo, High: hi, N: t.N, Open: t.Open}
}

func wilson(right, n int) (low, high float64) {
	nf := float64(n)
	p := float64(right) / nf
	const z = 1.96
	den := 1 + z*z/nf
	centre := (p + z*z/(2*nf)) / den
	half := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf)) / den
	return math.Max(0, centre-half), math.Min(1, centre+half)
}

// ElementRoute is what the measurements say about one element, and what
// follows from them.
type ElementRoute struct {
	Element string  `json:"element"`
	Target  float64 `json:"target"`
	Cost    string  `json:"cost"`
	// Custom is whether the target was set by a person.
	Custom bool `json:"custom"`
	// Random is every settled random check of this element; Guessed is the
	// part of it that checked answers that went back as pre-filled.
	Random   Tally     `json:"random"`
	Guessed  Tally     `json:"guessed"`
	Targeted Tally     `json:"targeted"`
	Estimate *Estimate `json:"estimate,omitempty"`
	Status   string    `json:"status"`
	// Guess is whether the template may pre-fill it, and Why says why not.
	Guess bool   `json:"guess"`
	Why   string `json:"why"`
	// CheckRate is the share of conversations worth checking for this element.
	CheckRate float64 `json:"checkRate"`
	// guessesWrong is set when untouched guesses themselves failed checks,
	// which no sender's record excuses.
	guessesWrong bool
}

// Routing is the route for every element, and who has earned trust.
type Routing struct {
	Elements map[string]*ElementRoute `json:"elements"`
	trust    map[string]store.SenderRecord
}

func costWeight(c string) float64 {
	switch c {
	case CostHigh:
		return 3
	case CostLow:
		return 1
	}
	return 2
}

// LoadRouting measures every element against its target.
func LoadRouting() (*Routing, error) { return loadRouting("") }

func loadRouting(model string) (*Routing, error) {
	set, err := store.WorkTargets()
	if err != nil {
		return nil, err
	}
	outcomes, err := store.AuditOutcomes(time.Now().Add(-routeWindow), model)
	if err != nil {
		return nil, err
	}
	r := &Routing{Elements: map[string]*ElementRoute{}}
	get := func(name string) *ElementRoute {
		if e := r.Elements[name]; e != nil {
			return e
		}
		t, custom := set[name]
		if !custom {
			var ok bool
			if t, ok = DefaultTargets[name]; !ok {
				t = fallbackTarget
			}
		}
		e := &ElementRoute{Element: name, Target: t.Target, Cost: t.Cost, Custom: custom}
		r.Elements[name] = e
		return e
	}
	for name := range DefaultTargets {
		get(name)
	}
	for name := range set {
		get(name)
	}
	for _, o := range outcomes {
		e := get(o.Element)
		if o.Stratum != store.StratumRandom {
			e.Targeted.add(o)
			continue
		}
		e.Random.add(o)
		if o.Prefilled {
			e.Guessed.add(o)
		}
	}
	for _, e := range r.Elements {
		e.decide()
	}
	return r, nil
}

func (e *ElementRoute) decide() {
	e.Estimate = estimate(e.Random)
	allowed := 1 - e.Target
	rate := func(hi, med, lo float64) float64 {
		switch e.Cost {
		case CostHigh:
			return hi
		case CostLow:
			return lo
		}
		return med
	}
	switch {
	case e.Random.N < MinChecks:
		e.Status = StatusUnmeasured
		e.CheckRate = rate(0.15, 0.10, 0.05)
	case 1-e.Estimate.Accuracy > allowed:
		e.Status = StatusMissing
		e.CheckRate = rate(0.15, 0.12, 0.08)
	case 1-e.Estimate.Low <= allowed:
		e.Status = StatusMeeting
		e.CheckRate = rate(0.05, 0.03, 0.02)
	default:
		e.Status = StatusUncertain
		e.CheckRate = rate(0.10, 0.07, 0.05)
	}

	e.Guess, e.Why = true, "guessed where nothing suggests otherwise"
	if g := estimate(e.Guessed); g != nil && e.Guessed.N >= MinChecks && 1-g.Accuracy > allowed {
		e.Guess, e.Why, e.guessesWrong = false, "untouched guesses were wrong too often when checked", true
		return
	}
	if e.Status == StatusMissing {
		e.Guess, e.Why = false, "checked answers miss the target, so it is read rather than guessed"
	}
}

// MayGuess says whether the template may pre-fill an element for a
// conversation, given whether its sender is trusted.
func (r *Routing) MayGuess(element string, trustedSender bool) bool {
	e := r.Elements[element]
	if e == nil || e.Guess {
		return true
	}
	// A trusted sender earns the cheap path back, but not where the guesses
	// themselves were shown to be wrong.
	return trustedSender && !e.guessesWrong
}

// Trusted reports whether a sender's answers have passed TrustStreak checks
// in a row.
func (r *Routing) Trusted(sender string) bool {
	if sender == "" {
		return false
	}
	if r.trust == nil {
		r.trust = map[string]store.SenderRecord{}
	}
	rec, ok := r.trust[sender]
	if !ok {
		recs, err := store.SenderRecords([]string{sender})
		if err != nil {
			return false
		}
		rec = recs[sender]
		r.trust[sender] = rec
	}
	return rec.Streak >= TrustStreak
}

// CheckRate is the share of conversations to check: the highest any element
// asks for, since one check reads a conversation for every element at once.
func (r *Routing) CheckRate() float64 {
	best := 0.0
	for _, e := range r.Elements {
		best = math.Max(best, e.CheckRate)
	}
	return best
}

// checkWindow is the period checks due are counted over.
const checkWindow = 7 * 24 * time.Hour

// ChecksDue is how many conversations should be checked now: the check rate
// of what was read in the last week, less what was checked in it.
func (r *Routing) ChecksDue() (int, error) {
	since := time.Now().Add(-checkWindow)
	read, err := store.AnswersSince(since)
	if err != nil {
		return 0, err
	}
	checked, err := store.ChecksSince(since)
	if err != nil {
		return 0, err
	}
	due := int(math.Ceil(r.CheckRate()*float64(read))) - checked
	if due < 0 {
		due = 0
	}
	return due, nil
}

// SetTarget sets how right an element must be and what a wrong answer costs.
// A target or cost left zero keeps what it was.
func SetTarget(element string, target float64, cost string) error {
	r, err := LoadRouting()
	if err != nil {
		return err
	}
	cur := r.Elements[element]
	if cur == nil {
		if _, byName, err := elements.Installed(); err != nil || byName[element] == nil {
			return fmt.Errorf("no element named %q is installed", element)
		}
		cur = &ElementRoute{Target: fallbackTarget.Target, Cost: fallbackTarget.Cost}
	}
	if target == 0 {
		target = cur.Target
	}
	if cost == "" {
		cost = cur.Cost
	}
	if target < 0.5 || target > 1 {
		return fmt.Errorf("a target is a share of answers right, from 0.5 to 1, not %v", target)
	}
	switch cost {
	case CostHigh, CostMedium, CostLow:
	default:
		return fmt.Errorf("a cost is high, medium or low, not %q", cost)
	}
	return store.SetWorkTarget(store.WorkTarget{Element: element, Target: target, Cost: cost})
}
