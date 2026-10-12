package ews

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calendar"
)

// Writing a calendar item as EWS fields.
//
// EWS validates element order against its schema, so the order below is
// the schema's — Item's fields first (Subject, Sensitivity, Body, the
// reminder), then CalendarItem's (UID, Start, End, IsAllDayEvent,
// LegacyFreeBusyStatus, Location, attendees, Recurrence, the time zones).

// windowsZone is the Windows name Exchange expects for an IANA zone.
func windowsZone(iana string) string {
	if iana == "" || iana == "UTC" || iana == "Etc/UTC" {
		return "UTC"
	}
	for win, z := range calendar.WindowsZones() {
		if z == iana {
			return win
		}
	}
	// Not the territory default: compare offsets now and in six months,
	// which picks a Windows zone that behaves the same.
	loc, ok := calendar.LoadZone(iana)
	if !ok {
		return "UTC"
	}
	now := time.Now()
	_, a := now.In(loc).Zone()
	_, b := now.AddDate(0, 6, 0).In(loc).Zone()
	for win, z := range calendar.WindowsZones() {
		if l, ok := calendar.LoadZone(z); ok {
			_, x := now.In(l).Zone()
			_, y := now.AddDate(0, 6, 0).In(l).Zone()
			if x == a && y == b {
				return win
			}
		}
	}
	return "UTC"
}

func attendeeList(tag string, as []calendar.Attendee) string {
	if len(as) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<t:" + tag + ">")
	for _, a := range as {
		b.WriteString(`<t:Attendee><t:Mailbox>`)
		if a.Name != "" {
			b.WriteString(`<t:Name>` + esc(a.Name) + `</t:Name>`)
		}
		b.WriteString(`<t:EmailAddress>` + esc(a.Address) + `</t:EmailAddress></t:Mailbox></t:Attendee>`)
	}
	b.WriteString("</t:" + tag + ">")
	return b.String()
}

func split(ev calendar.Event) (required, optional []calendar.Attendee) {
	for _, a := range ev.Attendees {
		if strings.EqualFold(a.Address, ev.Organizer) {
			continue
		}
		if a.Role == "optional" {
			optional = append(optional, a)
		} else {
			required = append(required, a)
		}
	}
	return
}

func when(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// itemXML is a new calendar item's fields.
func itemXML(ev calendar.Event) (string, error) {
	recurrence, err := recurrenceXML(ev)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<t:Subject>` + esc(ev.Summary) + `</t:Subject>`)
	if ev.Private {
		b.WriteString(`<t:Sensitivity>Private</t:Sensitivity>`)
	}
	if ev.Description != "" {
		b.WriteString(`<t:Body BodyType="Text">` + esc(ev.Description) + `</t:Body>`)
	}
	if len(ev.Alarms) > 0 {
		b.WriteString(`<t:ReminderIsSet>true</t:ReminderIsSet><t:ReminderMinutesBeforeStart>` + strconv.Itoa(ev.Alarms[0]) + `</t:ReminderMinutesBeforeStart>`)
	} else {
		b.WriteString(`<t:ReminderIsSet>false</t:ReminderIsSet>`)
	}
	// The same UID everywhere, so the invitation copies in attendees' mail
	// and this event are one event.
	b.WriteString(`<t:UID>` + esc(ev.UID) + `</t:UID>`)
	b.WriteString(`<t:Start>` + when(ev.Start) + `</t:Start><t:End>` + when(ev.End) + `</t:End>`)
	b.WriteString(`<t:IsAllDayEvent>` + strconv.FormatBool(ev.AllDay) + `</t:IsAllDayEvent>`)
	b.WriteString(`<t:LegacyFreeBusyStatus>` + freeBusy(ev) + `</t:LegacyFreeBusyStatus>`)
	if ev.Location != "" {
		b.WriteString(`<t:Location>` + esc(ev.Location) + `</t:Location>`)
	}
	req, opt := split(ev)
	b.WriteString(attendeeList("RequiredAttendees", req))
	b.WriteString(attendeeList("OptionalAttendees", opt))
	b.WriteString(recurrence)
	tz := windowsZone(zoneOf(ev))
	b.WriteString(`<t:StartTimeZone Id="` + esc(tz) + `"/><t:EndTimeZone Id="` + esc(tz) + `"/>`)
	return b.String(), nil
}

func zoneOf(ev calendar.Event) string {
	if ev.AllDay {
		return ""
	}
	return ev.TZ
}

func freeBusy(ev calendar.Event) string {
	if ev.ShowAs == "free" {
		return "Free"
	}
	if ev.Status == "tentative" {
		return "Tentative"
	}
	return "Busy"
}

// setFields is an edit, as SetItemField and DeleteItemField updates.
func setFields(ev calendar.Event, occurrence bool) (string, error) {
	var b strings.Builder
	set := func(uri, inner string) {
		fmt.Fprintf(&b, `<t:SetItemField><t:FieldURI FieldURI="%s"/><t:CalendarItem>%s</t:CalendarItem></t:SetItemField>`, uri, inner)
	}
	del := func(uri string) {
		fmt.Fprintf(&b, `<t:DeleteItemField><t:FieldURI FieldURI="%s"/></t:DeleteItemField>`, uri)
	}
	set("item:Subject", `<t:Subject>`+esc(ev.Summary)+`</t:Subject>`)
	if ev.Private {
		set("item:Sensitivity", `<t:Sensitivity>Private</t:Sensitivity>`)
	} else {
		set("item:Sensitivity", `<t:Sensitivity>Normal</t:Sensitivity>`)
	}
	set("item:Body", `<t:Body BodyType="Text">`+esc(ev.Description)+`</t:Body>`)
	if len(ev.Alarms) > 0 {
		set("item:ReminderIsSet", `<t:ReminderIsSet>true</t:ReminderIsSet>`)
		set("item:ReminderMinutesBeforeStart", `<t:ReminderMinutesBeforeStart>`+strconv.Itoa(ev.Alarms[0])+`</t:ReminderMinutesBeforeStart>`)
	} else {
		set("item:ReminderIsSet", `<t:ReminderIsSet>false</t:ReminderIsSet>`)
	}
	set("calendar:Start", `<t:Start>`+when(ev.Start)+`</t:Start>`)
	set("calendar:End", `<t:End>`+when(ev.End)+`</t:End>`)
	set("calendar:IsAllDayEvent", `<t:IsAllDayEvent>`+strconv.FormatBool(ev.AllDay)+`</t:IsAllDayEvent>`)
	set("calendar:LegacyFreeBusyStatus", `<t:LegacyFreeBusyStatus>`+freeBusy(ev)+`</t:LegacyFreeBusyStatus>`)
	set("calendar:Location", `<t:Location>`+esc(ev.Location)+`</t:Location>`)
	req, opt := split(ev)
	if len(req) > 0 {
		set("calendar:RequiredAttendees", attendeeList("RequiredAttendees", req))
	} else {
		del("calendar:RequiredAttendees")
	}
	if len(opt) > 0 {
		set("calendar:OptionalAttendees", attendeeList("OptionalAttendees", opt))
	} else {
		del("calendar:OptionalAttendees")
	}
	if !occurrence {
		rec, err := recurrenceXML(ev)
		if err != nil {
			return "", err
		}
		if rec != "" {
			set("calendar:Recurrence", rec)
		} else {
			del("calendar:Recurrence")
		}
		tz := windowsZone(zoneOf(ev))
		set("calendar:StartTimeZone", `<t:StartTimeZone Id="`+esc(tz)+`"/>`)
		set("calendar:EndTimeZone", `<t:EndTimeZone Id="`+esc(tz)+`"/>`)
	}
	return b.String(), nil
}

var dayNames = map[string]string{"MO": "Monday", "TU": "Tuesday", "WE": "Wednesday", "TH": "Thursday",
	"FR": "Friday", "SA": "Saturday", "SU": "Sunday"}

var indexNames = map[int]string{1: "First", 2: "Second", 3: "Third", 4: "Fourth", -1: "Last"}

// ErrUnsupportedRule is a repeat pattern EWS has no way to say.
var ErrUnsupportedRule = errors.New("Exchange cannot store this repeat pattern; choose a simpler one")

// recurrenceXML translates an RRULE into EWS's patterns. EWS has fewer than
// RFC 5545 — no BYSETPOS lists, no hourly — and refusing plainly is better
// than sending a different rule.
func recurrenceXML(ev calendar.Event) (string, error) {
	var rule string
	for _, l := range ev.Recurrence {
		if strings.HasPrefix(strings.ToUpper(l), "RRULE:") {
			rule = l[6:]
		}
	}
	if rule == "" {
		return "", nil
	}
	parts := map[string]string{}
	for _, kv := range strings.Split(rule, ";") {
		k, v, _ := strings.Cut(kv, "=")
		parts[strings.ToUpper(k)] = strings.ToUpper(v)
	}
	interval := parts["INTERVAL"]
	if interval == "" {
		interval = "1"
	}
	loc := time.UTC
	if l, ok := calendar.LoadZone(ev.TZ); ok {
		loc = l
	}
	start := ev.Start.In(loc)
	days := func(list string) (string, error) {
		var names []string
		for _, d := range strings.Split(list, ",") {
			name, ok := dayNames[strings.TrimLeft(d, "+-0123456789")]
			if !ok {
				return "", ErrUnsupportedRule
			}
			names = append(names, name)
		}
		return strings.Join(names, " "), nil
	}
	var pattern string
	switch parts["FREQ"] {
	case "DAILY":
		pattern = `<t:DailyRecurrence><t:Interval>` + interval + `</t:Interval></t:DailyRecurrence>`
	case "WEEKLY":
		byday := parts["BYDAY"]
		if byday == "" {
			byday = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}[start.Weekday()]
		}
		d, err := days(byday)
		if err != nil {
			return "", err
		}
		pattern = `<t:WeeklyRecurrence><t:Interval>` + interval + `</t:Interval><t:DaysOfWeek>` + d + `</t:DaysOfWeek></t:WeeklyRecurrence>`
	case "MONTHLY":
		if byday := parts["BYDAY"]; byday != "" {
			n, name, err := nthDay(byday, parts["BYSETPOS"])
			if err != nil {
				return "", err
			}
			pattern = `<t:RelativeMonthlyRecurrence><t:Interval>` + interval + `</t:Interval><t:DaysOfWeek>` + name +
				`</t:DaysOfWeek><t:DayOfWeekIndex>` + n + `</t:DayOfWeekIndex></t:RelativeMonthlyRecurrence>`
		} else {
			day := parts["BYMONTHDAY"]
			if day == "" {
				day = strconv.Itoa(start.Day())
			}
			if _, err := strconv.Atoi(day); err != nil || strings.HasPrefix(day, "-") {
				return "", ErrUnsupportedRule
			}
			pattern = `<t:AbsoluteMonthlyRecurrence><t:Interval>` + interval + `</t:Interval><t:DayOfMonth>` + day + `</t:DayOfMonth></t:AbsoluteMonthlyRecurrence>`
		}
	case "YEARLY":
		month := start.Month().String()
		if m := parts["BYMONTH"]; m != "" {
			n, err := strconv.Atoi(m)
			if err != nil || n < 1 || n > 12 {
				return "", ErrUnsupportedRule
			}
			month = time.Month(n).String()
		}
		if byday := parts["BYDAY"]; byday != "" {
			n, name, err := nthDay(byday, parts["BYSETPOS"])
			if err != nil {
				return "", err
			}
			pattern = `<t:RelativeYearlyRecurrence><t:DaysOfWeek>` + name + `</t:DaysOfWeek><t:DayOfWeekIndex>` + n +
				`</t:DayOfWeekIndex><t:Month>` + month + `</t:Month></t:RelativeYearlyRecurrence>`
		} else {
			day := parts["BYMONTHDAY"]
			if day == "" {
				day = strconv.Itoa(start.Day())
			}
			pattern = `<t:AbsoluteYearlyRecurrence><t:DayOfMonth>` + day + `</t:DayOfMonth><t:Month>` + month + `</t:Month></t:AbsoluteYearlyRecurrence>`
		}
	default:
		return "", ErrUnsupportedRule
	}
	startDate := start.Format("2006-01-02")
	var rng string
	switch {
	case parts["COUNT"] != "":
		rng = `<t:NumberedRecurrence><t:StartDate>` + startDate + `</t:StartDate><t:NumberOfOccurrences>` + parts["COUNT"] + `</t:NumberOfOccurrences></t:NumberedRecurrence>`
	case parts["UNTIL"] != "":
		until, err := time.Parse("20060102T150405Z", parts["UNTIL"])
		if err != nil {
			until, err = time.Parse("20060102", parts["UNTIL"])
		}
		if err != nil {
			return "", ErrUnsupportedRule
		}
		rng = `<t:EndDateRecurrence><t:StartDate>` + startDate + `</t:StartDate><t:EndDate>` + until.In(loc).Format("2006-01-02") + `</t:EndDate></t:EndDateRecurrence>`
	default:
		rng = `<t:NoEndRecurrence><t:StartDate>` + startDate + `</t:StartDate></t:NoEndRecurrence>`
	}
	return `<t:Recurrence>` + pattern + rng + `</t:Recurrence>`, nil
}

// nthDay reads "2TU", "-1FR" or "TU" with BYSETPOS=2 into EWS's index and day.
func nthDay(byday, setpos string) (string, string, error) {
	if strings.Contains(byday, ",") {
		return "", "", ErrUnsupportedRule
	}
	num := strings.TrimRight(byday, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if num == "" {
		num = setpos
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return "", "", ErrUnsupportedRule
	}
	idx, ok := indexNames[n]
	name, ok2 := dayNames[byday[len(strings.TrimRight(byday, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")):]]
	if !ok || !ok2 {
		return "", "", ErrUnsupportedRule
	}
	return idx, name, nil
}
