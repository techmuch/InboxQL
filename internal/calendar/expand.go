package calendar

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Occurrence is one time a series happens.
type Occurrence struct {
	Start, End time.Time
	AllDay     bool
	// Override is the changed occurrence that replaced this one, if any;
	// Start and End are then the override's.
	Override *Event
}

// MaxOccurrences stops a rule with no end from filling a window one minute
// at a time: FREQ=MINUTELY with no COUNT is legal, and nobody means it.
const MaxOccurrences = 5000

// Expand lists a series' occurrences that overlap [from, to), with the
// overrides applied: an override moves or changes the occurrence it names,
// and one marked cancelled removes it.
//
// The rule is evaluated in the event's own zone, so a weekly 9:00 meeting
// stays at 9:00 across a daylight-saving change, as everyone means it to.
func Expand(ev Event, overrides []Event, from, to time.Time) ([]Occurrence, error) {
	dur := ev.End.Sub(ev.Start)
	if dur < 0 {
		dur = 0
	}
	byRecurrence := map[int64]*Event{}
	for i := range overrides {
		byRecurrence[overrides[i].RecurrenceID.UTC().Unix()] = &overrides[i]
	}

	var starts []time.Time
	if len(ev.Recurrence) == 0 {
		starts = []time.Time{ev.Start}
	} else {
		set, err := ruleSet(ev)
		if err != nil {
			return nil, err
		}
		// Widen the search by the event's length, so an occurrence that began
		// before the window and is still going is found.
		next := set.Iterator()
		for n := 0; n < MaxOccurrences; n++ {
			t, ok := next()
			if !ok || !t.Before(to) {
				break
			}
			if t.Add(dur).After(from) || (dur == 0 && !t.Before(from)) {
				starts = append(starts, t)
			}
		}
	}

	var out []Occurrence
	seen := map[int64]bool{}
	for _, s := range starts {
		key := s.UTC().Unix()
		seen[key] = true
		if o, ok := byRecurrence[key]; ok {
			if o.Status == "cancelled" {
				continue
			}
			out = append(out, Occurrence{Start: o.Start, End: o.End, AllDay: o.AllDay, Override: o})
			continue
		}
		out = append(out, Occurrence{Start: s, End: s.Add(dur), AllDay: ev.AllDay})
	}
	// An override can move an occurrence into the window from outside it, or
	// name one the rule no longer produces; it still happened.
	for key, o := range byRecurrence {
		if seen[key] || o.Status == "cancelled" {
			continue
		}
		if o.End.After(from) && o.Start.Before(to) {
			out = append(out, Occurrence{Start: o.Start, End: o.End, AllDay: o.AllDay, Override: o})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func ruleSet(ev Event) (*rrule.Set, error) {
	loc := time.UTC
	if ev.TZ != "" {
		if l, ok := LoadZone(ev.TZ); ok {
			loc = l
		}
	}
	start := ev.Start.In(loc)
	set := &rrule.Set{}
	set.DTStart(start)
	for _, line := range ev.Recurrence {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToUpper(name) {
		case "RRULE":
			opt, err := rrule.StrToROptionInLocation(value, loc)
			if err != nil {
				return nil, fmt.Errorf("recurrence rule %q: %w", value, err)
			}
			opt.Dtstart = start
			r, err := rrule.NewRRule(*opt)
			if err != nil {
				return nil, fmt.Errorf("recurrence rule %q: %w", value, err)
			}
			set.RRule(r)
		case "RDATE", "EXDATE":
			for _, v := range strings.Split(value, ",") {
				t, err := time.Parse("20060102T150405Z", v)
				if err != nil {
					continue
				}
				if strings.EqualFold(name, "RDATE") {
					set.RDate(t.In(loc))
				} else {
					set.ExDate(t.In(loc))
				}
			}
		}
	}
	return set, nil
}

// Describe says how a series repeats, in words, for the event card: "Weekly
// on Monday and Wednesday", "Every 2 weeks", "Monthly until 3 March 2027".
func Describe(recurrence []string) string {
	for _, line := range recurrence {
		name, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(name, "RRULE") {
			continue
		}
		opt, err := rrule.StrToROption(value)
		if err != nil {
			return "Repeats"
		}
		unit := map[rrule.Frequency][2]string{
			rrule.YEARLY: {"Yearly", "years"}, rrule.MONTHLY: {"Monthly", "months"},
			rrule.WEEKLY: {"Weekly", "weeks"}, rrule.DAILY: {"Daily", "days"},
			rrule.HOURLY: {"Hourly", "hours"}, rrule.MINUTELY: {"Every minute", "minutes"},
		}[opt.Freq]
		s := unit[0]
		if opt.Interval > 1 {
			s = fmt.Sprintf("Every %d %s", opt.Interval, unit[1])
		}
		if len(opt.Byweekday) > 0 {
			names := []string{}
			for _, d := range opt.Byweekday {
				names = append(names, time.Weekday((d.Day()+1)%7).String())
			}
			s += " on " + joinAnd(names)
		}
		switch {
		case !opt.Until.IsZero():
			s += " until " + opt.Until.Format("2 January 2006")
		case opt.Count > 0:
			s += fmt.Sprintf(", %d times", opt.Count)
		}
		return s
	}
	return ""
}

func joinAnd(xs []string) string {
	if len(xs) <= 1 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// RemainingCount is, for a series whose rule ends after COUNT occurrences,
// how many of them fall at or after t — what a new series starting at t
// must carry so that splitting a series does not make it longer. ok is false
// when the rule has no COUNT.
//
// COUNT counts the rule's own instances, before any EXDATE removes one, so
// the instances are counted from the rule alone.
func RemainingCount(ev Event, t time.Time) (remaining int, ok bool) {
	for _, line := range ev.Recurrence {
		name, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(name, "RRULE") {
			continue
		}
		loc := time.UTC
		if ev.TZ != "" {
			if l, ok := LoadZone(ev.TZ); ok {
				loc = l
			}
		}
		opt, err := rrule.StrToROptionInLocation(value, loc)
		if err != nil || opt.Count == 0 {
			return 0, false
		}
		opt.Dtstart = ev.Start.In(loc)
		r, err := rrule.NewRRule(*opt)
		if err != nil {
			return 0, false
		}
		before := len(r.Between(opt.Dtstart.Add(-time.Second), t.Add(-time.Second), true))
		if n := opt.Count - before; n > 0 {
			return n, true
		}
		return 0, true
	}
	return 0, false
}
