package work

import (
	"math"
	"sort"
	"time"

	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
)

// # Measuring the loop
//
// Two questions, kept apart because they trade against each other:
//
// Efficiency — how much reading it took: conversations stored per day, how
// often submissions were refused and why (rework), leases that ran out
// unanswered, time from lease to submit, what the harness says it cost, and
// how many answers went back as the template guessed them.
//
// Accuracy — how often the answers are right: random checks (the only honest
// estimate), your random reviews, whether confidence means what it says, and
// what you did with what the loop proposed or did — cards and events approved
// or rejected, labels taken off, conversations moved back to the inbox.

// Metrics is everything the Work loop page shows.
type Metrics struct {
	Since  time.Time `json:"since"`
	Model  string    `json:"model,omitempty"`
	Models []string  `json:"models"`

	Reading Reading `json:"reading"`
	Checks  Checks  `json:"checks"`
	// Accuracy is over every random check, every element together.
	Accuracy *Estimate        `json:"accuracy,omitempty"`
	Elements []ElementMetrics `json:"elements"`
	// Actions is what became of labels, archiving, cards and events.
	Actions     map[string]store.WorkOutcome `json:"actions"`
	Calibration Calibration                  `json:"calibration"`
	// Flagged counts conversations waiting for a deep read.
	Flagged int `json:"flagged"`
}

// Reading is the efficiency side.
type Reading struct {
	*store.WorkSubmissionStats
	Batches int `json:"batches"`
	Leased  int `json:"leased"`
	Expired int `json:"expired"`
	// RefusalRate is refused over all submissions.
	RefusalRate float64 `json:"refusalRate"`
	// PerDay is conversations stored per day over the period's active days.
	PerDay float64 `json:"perDay"`
	// GuessedShare is the share of answers that went back as pre-filled.
	GuessedShare float64   `json:"guessedShare"`
	Usage        *UsagePer `json:"usage,omitempty"`
}

// UsagePer is what reading cost, per conversation stored, over the batches
// that reported it.
type UsagePer struct {
	Batches       int     `json:"batches"`
	Conversations int     `json:"conversations"`
	Tokens        float64 `json:"tokensPerConversation"`
	Cost          float64 `json:"costPerConversation"`
	Seconds       float64 `json:"secondsPerConversation"`
}

// Checks is the checking side.
type Checks struct {
	Random   Tally `json:"random"`
	Targeted Tally `json:"targeted"`
	// Open counts disagreements waiting for a person.
	Open int `json:"open"`
	// Due is how many conversations should be checked now; Rate the share
	// of reading worth checking.
	Due  int     `json:"due"`
	Rate float64 `json:"rate"`
}

// ElementMetrics is one element's row.
type ElementMetrics struct {
	Element string `json:"element"`
	About   string `json:"about"`
	Unit    string `json:"unit"`
	store.ElementAnswerStat
	// Reviewed and ReviewedRight are your rulings from the random review
	// queue on the loop's answers.
	Reviewed      int           `json:"reviewed"`
	ReviewedRight int           `json:"reviewedRight"`
	Route         *ElementRoute `json:"route"`
}

// Calibration compares how sure answers said they were with how often they
// were right, on random checks.
type Calibration struct {
	Bins []CalBin `json:"bins"`
	// Unrated counts checked answers that gave no confidence.
	Unrated int `json:"unrated"`
}

// CalBin is one band of stated confidence.
type CalBin struct {
	Lo    float64 `json:"lo"`
	Hi    float64 `json:"hi"`
	N     int     `json:"n"`
	Right int     `json:"right"`
	// Mean is the average confidence stated in the band.
	Mean float64 `json:"mean"`
}

var calEdges = []float64{0, 0.5, 0.7, 0.8, 0.9, 1.0001}

// Measure gathers the metrics since a time, for one model's answers or all.
func Measure(since time.Time, model string) (*Metrics, error) {
	m := &Metrics{Since: since, Model: model, Actions: map[string]store.WorkOutcome{}}
	var err error
	if m.Models, err = store.WorkModels(); err != nil {
		return nil, err
	}
	if m.Models == nil {
		m.Models = []string{}
	}

	// Efficiency.
	subs, err := store.SubmissionStats(since, model)
	if err != nil {
		return nil, err
	}
	batches, err := store.BatchStats(since, model, LeaseFor)
	if err != nil {
		return nil, err
	}
	m.Reading = Reading{WorkSubmissionStats: subs, Batches: batches.Batches, Leased: batches.Leased, Expired: batches.Expired}
	if total := subs.Stored + subs.Refused; total > 0 {
		m.Reading.RefusalRate = float64(subs.Refused) / float64(total)
	}
	active := 0
	for _, d := range subs.Days {
		if d.Stored > 0 {
			active++
		}
	}
	if active > 0 {
		m.Reading.PerDay = float64(subs.Stored) / float64(active)
	}
	if batches.UsageBatches > 0 && batches.Conversations > 0 {
		n := float64(batches.Conversations)
		m.Reading.Usage = &UsagePer{Batches: batches.UsageBatches, Conversations: batches.Conversations,
			Tokens: float64(batches.Usage.InputTokens+batches.Usage.OutputTokens) / n,
			Cost:   batches.Usage.CostUSD / n, Seconds: batches.Usage.Seconds / n}
	}

	// Accuracy.
	route, err := loadRouting(model)
	if err != nil {
		return nil, err
	}
	outcomes, err := store.AuditOutcomes(since, model)
	if err != nil {
		return nil, err
	}
	var all Tally
	for _, o := range outcomes {
		if o.Stratum == store.StratumRandom {
			m.Checks.Random.add(o)
			all.add(o)
		} else {
			m.Checks.Targeted.add(o)
		}
	}
	m.Accuracy = estimate(all)
	m.Calibration = calibrate(outcomes)
	if m.Checks.Open, err = store.CountOpenDisagreements(); err != nil {
		return nil, err
	}
	if m.Checks.Due, err = route.ChecksDue(); err != nil {
		return nil, err
	}
	m.Checks.Rate = route.CheckRate()
	if m.Flagged, err = store.CountFlags(); err != nil {
		return nil, err
	}
	if m.Actions, err = store.WorkActionOutcomes(since, model); err != nil {
		return nil, err
	}

	// By element.
	installed, byName, err := elements.Installed()
	if err != nil {
		return nil, err
	}
	answers, err := store.ElementAnswerStats(since, model)
	if err != nil {
		return nil, err
	}
	var answered, guessed int
	for _, e := range installed {
		row := ElementMetrics{Element: e.Name, About: e.About, Unit: e.Unit, ElementAnswerStat: answers[e.Name],
			Route: route.Elements[e.Name]}
		row.ElementAnswerStat.Element = e.Name
		if row.Route == nil {
			row.Route = &ElementRoute{Element: e.Name, Target: fallbackTarget.Target, Cost: fallbackTarget.Cost}
			row.Route.decide()
		}
		if byName[e.Name] != nil {
			if s, err := store.ScoreAnnotatorModel(e.Name, model); err == nil {
				row.Reviewed, row.ReviewedRight = s.Reviewed, s.Right
			}
		}
		answered += row.Answered
		guessed += row.Prefilled
		m.Elements = append(m.Elements, row)
	}
	if answered > 0 {
		m.Reading.GuessedShare = float64(guessed) / float64(answered)
	}
	return m, nil
}

func calibrate(outcomes []store.AuditOutcome) Calibration {
	c := Calibration{Bins: []CalBin{}}
	bins := make([]CalBin, len(calEdges)-1)
	sums := make([]float64, len(bins))
	for i := range bins {
		bins[i].Lo, bins[i].Hi = calEdges[i], math.Min(1, calEdges[i+1])
	}
	for _, o := range outcomes {
		if o.Stratum != store.StratumRandom || !o.Known {
			continue
		}
		if o.Confidence == nil {
			c.Unrated++
			continue
		}
		conf := *o.Confidence
		for i := range bins {
			if conf >= calEdges[i] && conf < calEdges[i+1] {
				bins[i].N++
				sums[i] += conf
				if o.Right {
					bins[i].Right++
				}
				break
			}
		}
	}
	for i := range bins {
		if bins[i].N > 0 {
			bins[i].Mean = sums[i] / float64(bins[i].N)
		}
		c.Bins = append(c.Bins, bins[i])
	}
	return c
}

// BacklogRow is how much of one element is still unanswered across the
// whole mailbox.
type BacklogRow struct {
	Element string `json:"element"`
	Pending int64  `json:"pending"`
	Total   int64  `json:"total"`
}

// Backlog counts what each element still has to answer. It reads the whole
// mailbox, so it is asked for separately from Measure.
func Backlog() ([]BacklogRow, error) {
	installed, byName, err := elements.Installed()
	if err != nil {
		return nil, err
	}
	var out []BacklogRow
	for _, e := range installed {
		a := byName[e.Name]
		if a == nil || e.Unit == elements.UnitSender {
			continue
		}
		p, err := store.Progress(a, a.Scope)
		if err != nil {
			return nil, err
		}
		out = append(out, BacklogRow{Element: e.Name, Pending: max(0, p.Total-p.Evaluated), Total: p.Total})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Element < out[j].Element })
	return out, nil
}
