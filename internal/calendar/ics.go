// Package calendar is the event model and the iCalendar it is read from and
// written to: parsing, writing, time zones, and expanding a recurring series
// into its occurrences.
//
// It knows nothing about storage or providers. Google, Exchange and the mail
// invitations all arrive here as iCalendar or are turned into this model, so
// recurrence and time zones are handled once.
package calendar

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
)

// Event is one VEVENT: a single event, a recurring series, or one changed
// occurrence of a series (RecurrenceID set).
type Event struct {
	UID           string
	Summary       string
	Description   string
	Location      string
	ConferenceURL string
	Start, End    time.Time
	AllDay        bool
	// TZ is the IANA zone the event is in, "" for UTC or a floating time.
	TZ string
	// Recurrence holds the RRULE, RDATE and EXDATE lines, normalised: dates
	// as UTC instants, so expanding never needs the original zone database.
	Recurrence []string
	// RecurrenceID is the original start of the occurrence this overrides.
	RecurrenceID time.Time
	Status       string // confirmed, tentative, cancelled
	ShowAs       string // busy, free
	Private      bool
	Organizer    string
	Attendees    []Attendee
	Sequence     int
	// Alarms are reminders, in minutes before the start.
	Alarms []int
}

// Attendee is one invited person.
type Attendee struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
	Role    string `json:"role"`   // required, optional, chair
	Status  string `json:"status"` // needs-action, accepted, tentative, declined
}

// Calendar is a parsed VCALENDAR: its METHOD (REQUEST, CANCEL, REPLY… for an
// invitation, empty for a plain file) and its events.
type Calendar struct {
	Method string
	Events []Event
}

// Parse reads iCalendar data.
func Parse(data []byte) (*Calendar, error) {
	cal, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("not iCalendar: %w", err)
	}
	out := &Calendar{Method: strings.ToUpper(propText(cal.Props, ical.PropMethod))}
	zones := customZones(cal)
	for _, child := range cal.Children {
		if child.Name != ical.CompEvent {
			continue
		}
		ev, err := parseEvent(child, zones)
		if err != nil {
			return nil, err
		}
		out.Events = append(out.Events, ev)
	}
	if len(out.Events) == 0 {
		return nil, errors.New("no events in this calendar")
	}
	return out, nil
}

func propText(props ical.Props, name string) string {
	p := props.Get(name)
	if p == nil {
		return ""
	}
	if t, err := p.Text(); err == nil {
		return t
	}
	return p.Value
}

// customZones reads VTIMEZONE blocks whose TZID is not a known name, as fixed
// offsets from their STANDARD part. Outlook writes "Customized Time Zone"
// and the like. A fixed offset is wrong across a daylight-saving change, and
// is still better than reading the time as UTC, which is wrong all year.
func customZones(cal *ical.Calendar) map[string]*time.Location {
	zones := map[string]*time.Location{}
	for _, c := range cal.Children {
		if c.Name != ical.CompTimezone {
			continue
		}
		id := propText(c.Props, ical.PropTimezoneID)
		if _, ok := LoadZone(id); ok || id == "" {
			continue
		}
		for _, part := range c.Children {
			if part.Name != ical.CompTimezoneStandard {
				continue
			}
			if off, ok := parseOffset(propText(part.Props, ical.PropTimezoneOffsetTo)); ok {
				zones[id] = time.FixedZone(id, off)
			}
		}
	}
	return zones
}

func parseOffset(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 5 {
		return 0, false
	}
	sign := 1
	if s[0] == '-' {
		sign = -1
	} else if s[0] != '+' {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[1:3])
	m, err2 := strconv.Atoi(s[3:5])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return sign * (h*3600 + m*60), true
}

// timeOf reads a DATE or DATE-TIME property: the instant, whether it was a
// date, and the zone name it was written in.
func timeOf(p *ical.Prop, zones map[string]*time.Location) (t time.Time, date bool, tz string, err error) {
	v := strings.TrimSpace(p.Value)
	if p.ValueType() == ical.ValueDate || len(v) == 8 {
		t, err = time.ParseInLocation("20060102", v, time.UTC)
		return t, true, "", err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.ParseInLocation("20060102T150405Z", v, time.UTC)
		return t, false, "", err
	}
	loc := time.UTC
	if id := p.Params.Get(ical.PropTimezoneID); id != "" {
		if l, ok := LoadZone(id); ok {
			loc, tz = l, l.String()
		} else if l, ok := zones[id]; ok {
			loc = l
		}
	}
	t, err = time.ParseInLocation("20060102T150405", v, loc)
	return t, false, tz, err
}

func parseEvent(c *ical.Component, zones map[string]*time.Location) (Event, error) {
	ev := Event{
		UID:         propText(c.Props, ical.PropUID),
		Summary:     propText(c.Props, ical.PropSummary),
		Description: propText(c.Props, ical.PropDescription),
		Location:    propText(c.Props, ical.PropLocation),
		Status:      strings.ToLower(propText(c.Props, ical.PropStatus)),
		ShowAs:      "busy",
	}
	if ev.UID == "" {
		return ev, errors.New("an event has no UID")
	}
	if ev.Status == "" {
		ev.Status = "confirmed"
	}
	if strings.EqualFold(propText(c.Props, ical.PropTransparency), "TRANSPARENT") {
		ev.ShowAs = "free"
	}
	if cls := strings.ToUpper(propText(c.Props, ical.PropClass)); cls == "PRIVATE" || cls == "CONFIDENTIAL" {
		ev.Private = true
	}
	if seq, err := strconv.Atoi(propText(c.Props, ical.PropSequence)); err == nil {
		ev.Sequence = seq
	}
	ev.ConferenceURL = conferenceURL(c, ev.Description, ev.Location)

	start := c.Props.Get(ical.PropDateTimeStart)
	if start == nil {
		return ev, fmt.Errorf("event %s has no start", ev.UID)
	}
	var err error
	if ev.Start, ev.AllDay, ev.TZ, err = timeOf(start, zones); err != nil {
		return ev, fmt.Errorf("event %s: start: %w", ev.UID, err)
	}
	switch {
	case c.Props.Get(ical.PropDateTimeEnd) != nil:
		if ev.End, _, _, err = timeOf(c.Props.Get(ical.PropDateTimeEnd), zones); err != nil {
			return ev, fmt.Errorf("event %s: end: %w", ev.UID, err)
		}
	case c.Props.Get(ical.PropDuration) != nil:
		d, err := c.Props.Get(ical.PropDuration).Duration()
		if err != nil {
			return ev, fmt.Errorf("event %s: duration: %w", ev.UID, err)
		}
		ev.End = ev.Start.Add(d)
	case ev.AllDay:
		ev.End = ev.Start.AddDate(0, 0, 1)
	default:
		ev.End = ev.Start
	}
	if rid := c.Props.Get(ical.PropRecurrenceID); rid != nil {
		if ev.RecurrenceID, _, _, err = timeOf(rid, zones); err != nil {
			return ev, fmt.Errorf("event %s: recurrence id: %w", ev.UID, err)
		}
	}

	for _, p := range c.Props.Values(ical.PropRecurrenceRule) {
		ev.Recurrence = append(ev.Recurrence, "RRULE:"+p.Value)
	}
	for _, name := range []string{ical.PropRecurrenceDates, ical.PropExceptionDates} {
		for _, p := range c.Props.Values(name) {
			var stamps []string
			for _, part := range strings.Split(p.Value, ",") {
				one := ical.NewProp(name)
				one.Params = p.Params
				one.Value = part
				t, _, _, err := timeOf(one, zones)
				if err != nil {
					continue
				}
				stamps = append(stamps, t.UTC().Format("20060102T150405Z"))
			}
			if len(stamps) > 0 {
				ev.Recurrence = append(ev.Recurrence, name+":"+strings.Join(stamps, ","))
			}
		}
	}

	if org := c.Props.Get(ical.PropOrganizer); org != nil {
		ev.Organizer = mailto(org.Value)
	}
	for _, p := range c.Props.Values(ical.PropAttendee) {
		a := Attendee{
			Address: mailto(p.Value),
			Name:    p.Params.Get(ical.ParamCommonName),
			Role:    "required",
			Status:  strings.ToLower(p.Params.Get(ical.ParamParticipationStatus)),
		}
		switch strings.ToUpper(p.Params.Get(ical.ParamRole)) {
		case "OPT-PARTICIPANT", "NON-PARTICIPANT":
			a.Role = "optional"
		case "CHAIR":
			a.Role = "chair"
		}
		if a.Status == "" {
			a.Status = "needs-action"
		}
		if a.Address != "" {
			ev.Attendees = append(ev.Attendees, a)
		}
	}
	for _, alarm := range c.Children {
		if alarm.Name != ical.CompAlarm {
			continue
		}
		if tr := alarm.Props.Get(ical.PropTrigger); tr != nil {
			if d, err := tr.Duration(); err == nil && d <= 0 {
				ev.Alarms = append(ev.Alarms, int(-d/time.Minute))
			}
		}
	}
	return ev, nil
}

func mailto(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(v), "mailto:") {
		v = v[len("mailto:"):]
	}
	return strings.ToLower(v)
}

// conferenceURL finds a meeting link: Google's and Microsoft's own
// properties first, then the first Meet, Teams, Zoom or Webex link in the
// location or description.
func conferenceURL(c *ical.Component, description, location string) string {
	for _, name := range []string{"X-GOOGLE-CONFERENCE", "X-MICROSOFT-SKYPETEAMSMEETINGURL", "URL"} {
		if v := propText(c.Props, name); strings.HasPrefix(v, "https://") {
			return v
		}
	}
	for _, text := range []string{location, description} {
		for _, f := range strings.Fields(text) {
			f = strings.Trim(f, "<>()[]\"'.,;")
			for _, host := range []string{"meet.google.com/", "teams.microsoft.com/", "zoom.us/", "webex.com/"} {
				if strings.HasPrefix(f, "https://") && strings.Contains(f, host) {
					return f
				}
			}
		}
	}
	return ""
}

// Encode writes events as an iCalendar file, with METHOD if one is given.
func Encode(method string, events []Event) ([]byte, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//InboxQL//Calendar//EN")
	if method != "" {
		cal.Props.SetText(ical.PropMethod, method)
	}
	stamp := time.Now().UTC()
	// A VTIMEZONE for every zone an event is written in, as RFC 5545 asks.
	zones := map[string]int{}
	for _, ev := range events {
		if ev.TZ != "" && !ev.AllDay {
			if _, seen := zones[ev.TZ]; !seen {
				zones[ev.TZ] = ev.Start.Year()
			}
		}
	}
	for tz, year := range zones {
		if vt, err := vtimezone(tz, year); err == nil {
			cal.Children = append(cal.Children, vt)
		}
	}
	for _, ev := range events {
		cal.Children = append(cal.Children, encodeEvent(ev, stamp))
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func setTime(props ical.Props, name string, t time.Time, allDay bool, tz string) {
	p := ical.NewProp(name)
	switch {
	case allDay:
		p.SetDate(t)
	case tz != "":
		if loc, ok := LoadZone(tz); ok {
			p.SetDateTime(t.In(loc))
		} else {
			p.SetDateTime(t.UTC())
		}
	default:
		p.SetDateTime(t.UTC())
	}
	props.Set(p)
}

func encodeEvent(ev Event, stamp time.Time) *ical.Component {
	c := ical.NewComponent(ical.CompEvent)
	c.Props.SetText(ical.PropUID, ev.UID)
	c.Props.SetDateTime(ical.PropDateTimeStamp, stamp)
	setTime(c.Props, ical.PropDateTimeStart, ev.Start, ev.AllDay, ev.TZ)
	setTime(c.Props, ical.PropDateTimeEnd, ev.End, ev.AllDay, ev.TZ)
	if !ev.RecurrenceID.IsZero() {
		setTime(c.Props, ical.PropRecurrenceID, ev.RecurrenceID, ev.AllDay, ev.TZ)
	}
	if ev.Summary != "" {
		c.Props.SetText(ical.PropSummary, ev.Summary)
	}
	if ev.Description != "" {
		c.Props.SetText(ical.PropDescription, ev.Description)
	}
	if ev.Location != "" {
		c.Props.SetText(ical.PropLocation, ev.Location)
	}
	if ev.ConferenceURL != "" {
		setRaw(c.Props, "URL", ev.ConferenceURL)
	}
	if ev.Status != "" {
		setRaw(c.Props, ical.PropStatus, strings.ToUpper(ev.Status))
	}
	if ev.ShowAs == "free" {
		setRaw(c.Props, ical.PropTransparency, "TRANSPARENT")
	}
	if ev.Private {
		setRaw(c.Props, ical.PropClass, "PRIVATE")
	}
	if ev.Sequence > 0 {
		// An integer, not text: SetText would write SEQUENCE;VALUE=TEXT, which
		// clients that order updates by sequence cannot compare.
		seq := ical.NewProp(ical.PropSequence)
		seq.Value = strconv.Itoa(ev.Sequence)
		c.Props.Set(seq)
	}
	for _, line := range ev.Recurrence {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		p := ical.NewProp(name)
		p.Value = value
		c.Props.Add(p)
	}
	if ev.Organizer != "" {
		p := ical.NewProp(ical.PropOrganizer)
		p.Value = "mailto:" + ev.Organizer
		c.Props.Set(p)
	}
	for _, a := range ev.Attendees {
		p := ical.NewProp(ical.PropAttendee)
		p.Value = "mailto:" + a.Address
		if a.Name != "" {
			p.Params.Set(ical.ParamCommonName, a.Name)
		}
		role := "REQ-PARTICIPANT"
		if a.Role == "optional" {
			role = "OPT-PARTICIPANT"
		} else if a.Role == "chair" {
			role = "CHAIR"
		}
		p.Params.Set(ical.ParamRole, role)
		if a.Status != "" {
			p.Params.Set(ical.ParamParticipationStatus, strings.ToUpper(a.Status))
		}
		c.Props.Add(p)
	}
	for _, m := range ev.Alarms {
		alarm := ical.NewComponent(ical.CompAlarm)
		setRaw(alarm.Props, ical.PropAction, "DISPLAY")
		alarm.Props.SetText(ical.PropDescription, "Reminder")
		tr := ical.NewProp(ical.PropTrigger)
		tr.SetDuration(-time.Duration(m) * time.Minute)
		alarm.Props.Set(tr)
		c.Children = append(c.Children, alarm)
	}
	return c
}

// Reply is the iMIP REPLY an attendee sends to answer an invitation: the
// same event, the same sequence, and only the one attendee with their answer.
func Reply(ev Event, me, status string) ([]byte, error) {
	r := ev
	r.Description, r.Alarms, r.Recurrence = "", nil, nil
	// In UTC: a reply is matched on its UID, and UTC needs no VTIMEZONE.
	r.TZ = ""
	r.Attendees = []Attendee{{Address: strings.ToLower(me), Role: "required", Status: status}}
	for _, a := range ev.Attendees {
		if strings.EqualFold(a.Address, me) {
			r.Attendees[0].Name, r.Attendees[0].Role = a.Name, a.Role
		}
	}
	return Encode("REPLY", []Event{r})
}

// setRaw writes a property's value as it is. SetText marks anything whose
// default type is not TEXT with VALUE=TEXT, which is wrong for a URL, an
// integer or an enumerated value, and some clients refuse it.
func setRaw(props ical.Props, name, value string) {
	p := ical.NewProp(name)
	p.Value = value
	props.Set(p)
}
