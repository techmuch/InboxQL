package calendar

import (
	"strings"
	"testing"
	"time"
)

// What Outlook sends: CRLF, a Windows zone name, a VTIMEZONE, a Teams link.
const outlookInvite = "BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nPRODID:Microsoft Exchange Server 2010\r\nVERSION:2.0\r\n" +
	"BEGIN:VTIMEZONE\r\nTZID:Pacific Standard Time\r\nBEGIN:STANDARD\r\nDTSTART:16010101T020000\r\nTZOFFSETFROM:-0700\r\nTZOFFSETTO:-0800\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
	"BEGIN:VEVENT\r\nORGANIZER;CN=Ada Lovelace:mailto:Ada@Example.com\r\n" +
	"ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;CN=Me:mailto:me@example.com\r\n" +
	"ATTENDEE;ROLE=OPT-PARTICIPANT;PARTSTAT=ACCEPTED:mailto:bob@example.com\r\n" +
	"SUMMARY:Engine review\r\nDTSTART;TZID=Pacific Standard Time:20261012T090000\r\nDTEND;TZID=Pacific Standard Time:20261012T100000\r\n" +
	"UID:040000008200E00074C5B7101A82E008\r\nRRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=6\r\nEXDATE;TZID=Pacific Standard Time:20261019T090000\r\n" +
	"SEQUENCE:2\r\nLOCATION:Microsoft Teams Meeting\r\nDESCRIPTION:Join: https://teams.microsoft.com/l/meetup-join/abc\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestParseOutlookInvite(t *testing.T) {
	cal, err := Parse([]byte(outlookInvite))
	if err != nil {
		t.Fatal(err)
	}
	if cal.Method != "REQUEST" || len(cal.Events) != 1 {
		t.Fatalf("method %q, %d events", cal.Method, len(cal.Events))
	}
	ev := cal.Events[0]
	if ev.TZ != "America/Los_Angeles" {
		t.Errorf("zone %q", ev.TZ)
	}
	if got := ev.Start.UTC().Format(time.RFC3339); got != "2026-10-12T16:00:00Z" {
		t.Errorf("start %s (9:00 Pacific daylight is 16:00 UTC)", got)
	}
	if ev.Organizer != "ada@example.com" || len(ev.Attendees) != 2 || ev.Attendees[1].Role != "optional" {
		t.Errorf("people %q %+v", ev.Organizer, ev.Attendees)
	}
	if ev.ConferenceURL != "https://teams.microsoft.com/l/meetup-join/abc" || ev.Sequence != 2 {
		t.Errorf("conference %q seq %d", ev.ConferenceURL, ev.Sequence)
	}
	if len(ev.Alarms) != 1 || ev.Alarms[0] != 15 {
		t.Errorf("alarms %v", ev.Alarms)
	}
	if Describe(ev.Recurrence) != "Weekly on Monday, 6 times" {
		t.Errorf("describe %q", Describe(ev.Recurrence))
	}
}

// A weekly 9:00 meeting stays at 9:00 local across the November change; the
// excluded date is gone; COUNT counts the excluded one too, per RFC 5545.
func TestExpandAcrossDaylightSaving(t *testing.T) {
	cal, _ := Parse([]byte(outlookInvite))
	occ, err := Expand(cal.Events[0], nil, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	la, _ := time.LoadLocation("America/Los_Angeles")
	var got []string
	for _, o := range occ {
		got = append(got, o.Start.In(la).Format("Jan 2 15:04"))
	}
	want := "Oct 12 09:00,Oct 26 09:00,Nov 2 09:00,Nov 9 09:00,Nov 16 09:00"
	if strings.Join(got, ",") != want {
		t.Errorf("got %v\nwant %s", got, want)
	}
}

func TestOverridesMoveAndCancel(t *testing.T) {
	cal, _ := Parse([]byte(outlookInvite))
	series := cal.Events[0]
	moved := series
	moved.RecurrenceID = time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC)
	moved.Start = time.Date(2026, 10, 27, 17, 0, 0, 0, time.UTC)
	moved.End = moved.Start.Add(time.Hour)
	moved.Recurrence = nil
	cancelled := series
	cancelled.RecurrenceID = time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC) // 9:00 PST
	cancelled.Status = "cancelled"
	occ, _ := Expand(series, []Event{moved, cancelled}, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	if len(occ) != 1 || occ[0].Override == nil || !occ[0].Start.Equal(moved.Start) {
		t.Fatalf("got %+v", occ)
	}
}

func TestAllDayAndDuration(t *testing.T) {
	data := "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:a\nSUMMARY:Holiday\nDTSTART;VALUE=DATE:20261225\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nUID:b\nDTSTART:20261001T100000Z\nDURATION:PT90M\nTRANSP:TRANSPARENT\nCLASS:PRIVATE\nEND:VEVENT\nEND:VCALENDAR\n"
	cal, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	h, b := cal.Events[0], cal.Events[1]
	if !h.AllDay || h.End.Sub(h.Start) != 24*time.Hour {
		t.Errorf("all-day %+v", h)
	}
	if b.End.Sub(b.Start) != 90*time.Minute || b.ShowAs != "free" || !b.Private {
		t.Errorf("duration event %+v", b)
	}
}

// A zone known only from its own VTIMEZONE block is read at its standard
// offset rather than as UTC.
func TestCustomZoneFallsBackToItsOffset(t *testing.T) {
	data := "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VTIMEZONE\nTZID:Customized Time Zone\nBEGIN:STANDARD\nDTSTART:16010101T000000\nTZOFFSETFROM:+0100\nTZOFFSETTO:+0100\nEND:STANDARD\nEND:VTIMEZONE\n" +
		"BEGIN:VEVENT\nUID:c\nDTSTART;TZID=Customized Time Zone:20261001T100000\nDTEND;TZID=Customized Time Zone:20261001T110000\nEND:VEVENT\nEND:VCALENDAR\n"
	cal, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := cal.Events[0].Start.UTC().Hour(); got != 9 {
		t.Errorf("hour %d, want 9 UTC", got)
	}
}

func TestEncodeRoundTripAndReply(t *testing.T) {
	cal, _ := Parse([]byte(outlookInvite))
	b, err := Encode("", cal.Events)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(b)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	a, z := cal.Events[0], again.Events[0]
	if !a.Start.Equal(z.Start) || a.TZ != z.TZ || len(z.Attendees) != 2 || strings.Join(a.Recurrence, "|") != strings.Join(z.Recurrence, "|") || len(z.Alarms) != 1 {
		t.Errorf("round trip lost something:\n%+v\n%+v", a, z)
	}
	r, err := Reply(a, "ME@example.com", "accepted")
	if err != nil {
		t.Fatal(err)
	}
	rep, _ := Parse(r)
	if strings.Contains(string(r), "VALUE=TEXT") {
		t.Errorf("a typed property was written as text:\n%s", r)
	}
	if rep.Method != "REPLY" || len(rep.Events[0].Attendees) != 1 || rep.Events[0].Attendees[0].Status != "accepted" || rep.Events[0].Sequence != 2 {
		t.Errorf("reply %+v\n%s", rep, r)
	}
}

func TestLoadZone(t *testing.T) {
	for in, want := range map[string]string{
		"Europe/Paris": "Europe/Paris", "W. Europe Standard Time": "Europe/Berlin",
		"/mozilla.org/20050126_1/America/New_York": "America/New_York", `"Tokyo Standard Time"`: "Asia/Tokyo",
	} {
		if loc, ok := LoadZone(in); !ok || loc.String() != want {
			t.Errorf("%q -> %v %v", in, loc, ok)
		}
	}
	if _, ok := LoadZone("Nowhere Standard Time"); ok {
		t.Error("an unknown zone loaded")
	}
}

func TestRemainingCount(t *testing.T) {
	start := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	ev := Event{Start: start, End: start.Add(time.Hour), Recurrence: []string{"RRULE:FREQ=WEEKLY;COUNT=6", "EXDATE:20261019T090000Z"}}
	if n, ok := RemainingCount(ev, start.AddDate(0, 0, 28)); !ok || n != 2 {
		t.Errorf("remaining %d %v, want 2 (the excluded date still counts)", n, ok)
	}
	if _, ok := RemainingCount(Event{Start: start, Recurrence: []string{"RRULE:FREQ=DAILY"}}, start); ok {
		t.Error("a rule with no count reported one")
	}
}

func TestVTimezoneForDaylightSaving(t *testing.T) {
	vt, err := vtimezone("America/Los_Angeles", 2026)
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, c := range vt.Children {
		parts[c.Name] = c.Props.Get("RRULE").Value + " " + c.Props.Get("DTSTART").Value + " " + c.Props.Get("TZOFFSETTO").Value
	}
	if parts["DAYLIGHT"] != "FREQ=YEARLY;BYMONTH=3;BYDAY=2SU 20260308T020000 -0700" {
		t.Errorf("daylight %q", parts["DAYLIGHT"])
	}
	if parts["STANDARD"] != "FREQ=YEARLY;BYMONTH=11;BYDAY=1SU 20261101T020000 -0800" {
		t.Errorf("standard %q", parts["STANDARD"])
	}
	// Europe changes on the last Sunday.
	eu, _ := vtimezone("Europe/London", 2026)
	for _, c := range eu.Children {
		if r := c.Props.Get("RRULE").Value; r != "FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU" && r != "FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU" {
			t.Errorf("london %s %s", c.Name, r)
		}
	}
	// A file with a zoned event carries its VTIMEZONE and reads back the same.
	cal, _ := Parse([]byte(outlookInvite))
	b, _ := Encode("", cal.Events)
	if !strings.Contains(string(b), "BEGIN:VTIMEZONE") || !strings.Contains(string(b), "TZID:America/Los_Angeles") {
		t.Errorf("no VTIMEZONE:\n%s", b)
	}
	r, _ := Reply(cal.Events[0], "me@example.com", "accepted")
	if strings.Contains(string(r), "TZID") {
		t.Errorf("reply carries a zone:\n%s", r)
	}
}
