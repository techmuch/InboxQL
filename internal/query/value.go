package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Now is the clock the language reads for relative dates. Overridable so tests
// of `after:7d` do not depend on when they run.
var Now = time.Now

// ParseDateValue resolves a date expression to the half-open millisecond range
// [start, end) it names.
//
// Accepted forms, in the order people reach for them:
//
//	2026-08-15   a day
//	2026-08      a month
//	2026         a year
//	today, yesterday
//	7d, 2w, 3m, 1y   that long ago, counting back from now
//
// Ranges rather than instants because "on:2026-08" has to mean the whole month,
// and comparing epoch milliseconds against an indexed column is far cheaper
// than the strftime() the old filters used, which could not use an index at all.
func ParseDateValue(v string) (start, end int64, err error) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return 0, 0, fmt.Errorf("empty date")
	}

	now := Now()

	switch v {
	case "today":
		d := truncateDay(now)
		return d.UnixMilli(), d.AddDate(0, 0, 1).UnixMilli(), nil
	case "yesterday":
		d := truncateDay(now).AddDate(0, 0, -1)
		return d.UnixMilli(), d.AddDate(0, 0, 1).UnixMilli(), nil
	}

	// Relative: a count followed by a unit.
	if len(v) >= 2 {
		unit := v[len(v)-1]
		if n, convErr := strconv.Atoi(v[:len(v)-1]); convErr == nil && n >= 0 {
			var t time.Time
			switch unit {
			case 'd':
				t = truncateDay(now).AddDate(0, 0, -n)
			case 'w':
				t = truncateDay(now).AddDate(0, 0, -7*n)
			case 'm':
				t = truncateDay(now).AddDate(0, -n, 0)
			case 'y':
				t = truncateDay(now).AddDate(-n, 0, 0)
			case 'h':
				t = now.Add(-time.Duration(n) * time.Hour)
			}
			if !t.IsZero() {
				return t.UnixMilli(), now.UnixMilli(), nil
			}
		}
	}

	for _, form := range []struct {
		layout string
		add    func(time.Time) time.Time
	}{
		{"2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }},
		{"2006/01/02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }},
		{"2006-01", func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }},
		{"2006", func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }},
	} {
		if t, parseErr := time.ParseInLocation(form.layout, v, time.Local); parseErr == nil {
			return t.UnixMilli(), form.add(t).UnixMilli(), nil
		}
	}

	return 0, 0, fmt.Errorf("%q is not a date (try 2026-08-15, 2026-08, today, or 7d)", v)
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ParseSize resolves a size expression to bytes.
//
//	5mb, 500kb, 1.5gb, 2048
//
// Decimal units, because mail clients quote attachment limits that way and a
// user checking "larger:25mb" against Gmail's limit should get Gmail's answer.
func ParseSize(v string) (int64, error) {
	v = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(v, " ", "")))
	if v == "" {
		return 0, fmt.Errorf("empty size")
	}

	multiplier := int64(1)
	for _, suffix := range []struct {
		s string
		m int64
	}{{"gb", 1000 * 1000 * 1000}, {"mb", 1000 * 1000}, {"kb", 1000}, {"g", 1000 * 1000 * 1000}, {"m", 1000 * 1000}, {"k", 1000}, {"b", 1}} {
		if strings.HasSuffix(v, suffix.s) {
			multiplier = suffix.m
			v = strings.TrimSuffix(v, suffix.s)
			break
		}
	}

	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("%q is not a size (try 5mb, 500kb, or a byte count)", v)
	}
	return int64(f * float64(multiplier)), nil
}

// bucketExpr is the SQL that truncates an epoch-milliseconds column to a
// calendar bucket, and the label it produces.
//
// Weeks start on Monday: '%W' counts from Sunday and produces a label that
// disagrees with every calendar the user owns. The -3 days / +4 days shift is
// the standard ISO-week trick.
func bucketExpr(col, bucket string) (string, error) {
	sec := col + " / 1000, 'unixepoch', 'localtime'"
	switch bucket {
	case "hour":
		return "strftime('%Y-%m-%d %H:00', " + sec + ")", nil
	case "day", "":
		return "strftime('%Y-%m-%d', " + sec + ")", nil
	case "week":
		return "strftime('%Y-%m-%d', " + col + " / 1000, 'unixepoch', 'localtime', '-3 days', 'weekday 1', '-7 days')", nil
	case "month":
		return "strftime('%Y-%m', " + sec + ")", nil
	case "year":
		return "strftime('%Y', " + sec + ")", nil
	default:
		return "", fmt.Errorf("%q is not a bucket (day, week, month, year, hour)", bucket)
	}
}
