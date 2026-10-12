package calendar

import (
	"fmt"
	"time"

	ical "github.com/emersion/go-ical"
)

// vtimezone writes the VTIMEZONE block RFC 5545 requires for every TZID a
// file uses. Most clients resolve an IANA name by themselves; Outlook is the
// one that may not, and an event whose zone it cannot read lands at the wrong
// hour.
//
// Built from Go's zone database: the offsets either side of the zone's two
// transitions in the given year, with the yearly rule that repeats them
// (the nth or last weekday of a month). A zone without daylight saving gets a
// single STANDARD part.
func vtimezone(tzid string, year int) (*ical.Component, error) {
	loc, ok := LoadZone(tzid)
	if !ok {
		return nil, fmt.Errorf("unknown zone %q", tzid)
	}
	c := ical.NewComponent(ical.CompTimezone)
	setRaw(c.Props, ical.PropTimezoneID, loc.String())

	jan := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	_, janOff := jan.Zone()
	jul := time.Date(year, 7, 1, 0, 0, 0, 0, loc)
	_, julOff := jul.Zone()
	if janOff == julOff {
		std := ical.NewComponent(ical.CompTimezoneStandard)
		setRaw(std.Props, ical.PropDateTimeStart, "19700101T000000")
		setRaw(std.Props, ical.PropTimezoneOffsetFrom, offset(janOff))
		setRaw(std.Props, ical.PropTimezoneOffsetTo, offset(janOff))
		name, _ := jan.Zone()
		setRaw(std.Props, ical.PropTimezoneName, name)
		c.Children = append(c.Children, std)
		return c, nil
	}
	for _, t := range transitions(loc, year) {
		before := t.Add(-time.Second)
		_, fromOff := before.Zone()
		name, toOff := t.Zone()
		kind := ical.CompTimezoneStandard
		if toOff > fromOff {
			kind = ical.CompTimezoneDaylight
		}
		part := ical.NewComponent(kind)
		// DTSTART is the local wall time the transition happens at, in the
		// offset in force before it.
		wall := t.Add(-time.Second).In(time.FixedZone("", fromOff)).Add(time.Second)
		setRaw(part.Props, ical.PropDateTimeStart, wall.Format("20060102T150405"))
		setRaw(part.Props, ical.PropTimezoneOffsetFrom, offset(fromOff))
		setRaw(part.Props, ical.PropTimezoneOffsetTo, offset(toOff))
		setRaw(part.Props, ical.PropTimezoneName, name)
		setRaw(part.Props, ical.PropRecurrenceRule, yearlyRule(wall))
		c.Children = append(c.Children, part)
	}
	return c, nil
}

// transitions finds the instants in a year at which a zone's offset changes.
func transitions(loc *time.Location, year int) []time.Time {
	var out []time.Time
	t := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, loc)
	_, prev := t.Zone()
	for d := t; d.Before(end); d = d.Add(24 * time.Hour) {
		_, off := d.Add(24 * time.Hour).Zone()
		if off == prev {
			continue
		}
		// Narrow to the minute within this day.
		lo, hi := d, d.Add(24*time.Hour)
		for hi.Sub(lo) > time.Minute {
			mid := lo.Add(hi.Sub(lo) / 2)
			if _, o := mid.Zone(); o == prev {
				lo = mid
			} else {
				hi = mid
			}
		}
		out = append(out, hi.Truncate(time.Minute))
		prev = off
	}
	return out
}

func offset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%02d%02d", sign, sec/3600, (sec%3600)/60)
}

// yearlyRule says "the nth (or last) weekday of this month", for the date.
func yearlyRule(d time.Time) string {
	day := [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}[d.Weekday()]
	n := (d.Day()-1)/7 + 1
	if d.AddDate(0, 0, 7).Month() != d.Month() {
		n = -1 // the last one, which is how most zones word it
	}
	return fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%d%s", int(d.Month()), n, day)
}
