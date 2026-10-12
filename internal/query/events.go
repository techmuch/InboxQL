package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Calendar events: `in:events`.
//
// A row is an occurrence — one time a meeting happens — not a series. "What
// is on next week" and "how many hours did Acme take" are questions about
// occurrences; a weekly series is fifty of them a year, and counting it once
// would make every total wrong.
//
// Aliases: o (event_occurrences), e (events), c (calendars).

// EntityEvent is a calendar event occurrence.
const EntityEvent = "event"

// Runs before fields.go's init (files initialise in name order), so these
// fields are appended to the registry in time to be indexed with the rest.
func init() {
	for _, k := range []string{"events", "event", "calendar", "meetings", "meeting"} {
		Entities[k] = EntityEvent
	}
	EntityNames = append(EntityNames, "events")
	for i := range Registry {
		// Logs held `duration:` alone, and `duration>1000` without `in:`
		// meant the log. Events claim the name too; marking the log's as
		// primary keeps that query meaning what it meant.
		if Registry[i].Entity == EntityLog && Registry[i].Name == "duration" {
			Registry[i].Primary = true
		}
	}
	Registry = append(Registry, eventFields...)
	entityAliases[EntityEvent] = map[string]string{"title": "summary", "subject": "summary", "with": "attendee"}
}

var eventFields = []Field{
	{Name: "calendar", Entity: EntityEvent, Type: TypeText, Ops: matchOps,
		Summary: "which calendar it is in, by name", Example: "in:events calendar:work"},
	{Name: "attendee", Entity: EntityEvent, Type: TypeAddress, Ops: matchOps, Values: ValuesAddresses,
		Summary: "someone invited; me() is you", Example: "in:events attendee:*@acme.com"},
	{Name: "organizer", Entity: EntityEvent, Type: TypeAddress, Ops: matchOps, Values: ValuesAddresses,
		Summary: "who set it up", Example: "in:events organizer:me()"},
	{Name: "status", Entity: EntityEvent, Type: TypeEnum, Ops: matchOps,
		Enum:    []string{"accepted", "tentative", "declined", "needs-action", "organizer"},
		Summary: "your answer to it", Example: "in:events status:needs-action"},
	{Name: "show", Entity: EntityEvent, Type: TypeEnum, Ops: matchOps, Enum: []string{"busy", "free"},
		Summary: "whether it blocks your time", Example: "in:events show:busy"},
	{Name: "summary", Entity: EntityEvent, Type: TypeText, Ops: matchOps,
		Summary: "words in the title", Example: "in:events title:standup"},
	{Name: "location", Entity: EntityEvent, Type: TypeText, Ops: matchOps,
		Summary: "where", Example: "in:events location:london"},
	{Name: "next", Entity: EntityEvent, Type: TypeText, Ops: []Op{OpMatch},
		Summary: "from now until this far ahead", Example: "in:events next:7d"},
	{Name: "during", Entity: EntityEvent, Type: TypeText, Ops: []Op{OpMatch},
		Summary: "going on at this moment, or on this day", Example: "in:events during:2026-10-15T14:00"},
	{Name: "duration", Entity: EntityEvent, Type: TypeNumber, Ops: compareOps,
		Summary: "length in minutes", Example: "in:events duration>60"},
	{Name: "has", Entity: EntityEvent, Type: TypeEnum, Ops: matchOps,
		Enum:    []string{"conference", "attendees", "recurrence", "reminder", "location", "charge", "thread"},
		Summary: "conference link, attendees, a repeat…", Example: "in:events -has:conference"},
	{Name: "is", Entity: EntityEvent, Type: TypeEnum, Ops: matchOps,
		Enum:    []string{"recurring", "allday", "private", "proposed", "cancelled", "invitation", "past", "upcoming"},
		Summary: "recurring, all-day, proposed…", Example: "in:events is:recurring"},
	{Name: "charge", Entity: EntityEvent, Type: TypeText, Ops: matchOps,
		Summary: "who the time is charged to (time tracking)", Example: "in:events charge:acme"},
	{Name: "id", Entity: EntityEvent, Type: TypeIdent, Ops: []Op{OpMatch},
		Summary: "the event's id", Example: "in:events id:ev_…"},
}

// eventBase is what every event query excludes: cancelled events, and the
// mail copy of an invitation that is also in a synced calendar.
const eventBase = `e.status != 'cancelled' AND c.hidden = 0
	AND NOT (c.provider = 'mail' AND EXISTS (SELECT 1 FROM events d JOIN calendars dc ON dc.id = d.calendar_id
		WHERE d.uid = e.uid AND dc.provider != 'mail'))`

const eventFrom = ` FROM event_occurrences o JOIN events e ON e.id = o.event_id JOIN calendars c ON c.id = e.calendar_id `

// EventSelectList is the columns an event row carries.
const EventSelectList = `o.event_id, e.calendar_id, c.name, COALESCE(c.color,''), e.uid, o.start_at, o.end_at, o.all_day,
	e.summary, e.location, e.conference_url, e.status, e.show_as, e.my_status,
	(e.rrule != '' OR e.recurrence_id != 0), e.proposed, e.organizer,
	(SELECT COUNT(*) FROM event_attendees a WHERE a.event_id = e.id), COALESCE(o.charge,''),
	COALESCE(e.thread_key,''), COALESCE(e.source_message_id,'')`

func (c *compiler) eventTerm(t *Term, negated bool) (string, error) {
	switch t.Field {
	case "in":
		return "1=1", nil
	case "calendar":
		return wrap("("+c.stringPredicate("c.name", t)+" OR c.id = "+c.arg(t.Value)+")", negated), nil
	case "summary", "title", "subject":
		return wrap(c.stringPredicate("e.summary", t), negated), nil
	case "location":
		return wrap(c.stringPredicate("e.location", t), negated), nil
	case "":
		// A bare word: the title, the place, the description.
		return wrap("("+c.stringPredicate("e.summary", t)+" OR "+c.stringPredicate("e.location", t)+
			" OR "+c.stringPredicate("e.description", t)+")", negated), nil
	case "attendee", "with":
		pred, err := c.addressOrSelf("ea.address", t)
		if err != nil {
			return "", err
		}
		return wrap("EXISTS (SELECT 1 FROM event_attendees ea WHERE ea.event_id = e.id AND "+pred+")", negated), nil
	case "organizer":
		pred, err := c.addressOrSelf("e.organizer", t)
		if err != nil {
			return "", err
		}
		return wrap(pred, negated), nil
	case "status":
		v := strings.ToLower(t.Value)
		if v == "needs-action" {
			// An invitation nobody has answered is "needs action" whether the
			// sender wrote the word or left it out.
			return wrap("(e.my_status IN ('needs-action', '') AND e.source_message_id IS NOT NULL)", negated), nil
		}
		return wrap("e.my_status = "+c.arg(v), negated), nil
	case "show":
		return wrap("e.show_as = "+c.arg(strings.ToLower(t.Value)), negated), nil
	case "charge":
		return wrap(c.stringPredicate("COALESCE(o.charge,'')", t), negated), nil
	case "id":
		return wrap("(e.id = "+c.arg(t.Value)+" OR e.uid = "+c.arg(t.Value)+")", negated), nil
	case "thread":
		return wrap("e.thread_key = (SELECT thread_key FROM messages WHERE id = "+c.arg(t.Value)+")", negated), nil
	case "after":
		start, _, err := ParseDateValue(t.Value)
		if err != nil {
			return "", fmt.Errorf("after: %w", err)
		}
		return wrap("o.start_at >= "+c.arg(start), negated), nil
	case "before":
		start, _, err := ParseDateValue(t.Value)
		if err != nil {
			return "", fmt.Errorf("before: %w", err)
		}
		return wrap("o.start_at < "+c.arg(start), negated), nil
	case "on":
		start, end, err := ParseDateValue(t.Value)
		if err != nil {
			return "", fmt.Errorf("on: %w", err)
		}
		return wrap("(o.end_at > "+c.arg(start)+" AND o.start_at < "+c.arg(end)+")", negated), nil
	case "next":
		d, err := parseAhead(t.Value)
		if err != nil {
			return "", fmt.Errorf("next: %w", err)
		}
		now := time.Now()
		return wrap("(o.end_at > "+c.arg(now.UnixMilli())+" AND o.start_at < "+c.arg(now.Add(d).UnixMilli())+")", negated), nil
	case "during":
		start, end, err := parseMoment(t.Value)
		if err != nil {
			return "", fmt.Errorf("during: %w", err)
		}
		return wrap("(o.end_at > "+c.arg(start)+" AND o.start_at < "+c.arg(end)+")", negated), nil
	case "duration":
		mins, err := strconv.ParseFloat(strings.TrimSpace(t.Value), 64)
		if err != nil {
			return "", fmt.Errorf("duration: %q is not a number of minutes", t.Value)
		}
		op := map[Op]string{OpGreater: ">", OpGreaterOrEqual: ">=", OpLess: "<", OpLessOrEqual: "<="}[t.Op]
		if op == "" {
			op = "="
		}
		return wrap("(o.end_at - o.start_at) / 60000.0 "+op+" "+c.arg(mins), negated), nil
	case "has":
		col := map[string]string{
			"conference": "e.conference_url != ''",
			"attendees":  "EXISTS (SELECT 1 FROM event_attendees ea WHERE ea.event_id = e.id)",
			"recurrence": "(e.rrule != '' OR e.recurrence_id != 0)",
			"reminder":   "e.alarms != '[]'",
			"location":   "e.location != ''",
			"charge":     "o.charge IS NOT NULL AND o.charge != ''",
			"thread":     "e.thread_key IS NOT NULL",
		}[strings.ToLower(t.Value)]
		if col == "" {
			return "", fmt.Errorf("has: %q is not something an event has (conference, attendees, recurrence, reminder, location, charge, thread)", t.Value)
		}
		return wrap(col, negated), nil
	case "is":
		now := c.arg(time.Now().UnixMilli())
		col := map[string]string{
			"recurring":  "(e.rrule != '' OR e.recurrence_id != 0)",
			"allday":     "o.all_day = 1",
			"private":    "e.private = 1",
			"proposed":   "e.proposed = 1",
			"cancelled":  "e.status = 'cancelled'",
			"invitation": "e.source_message_id IS NOT NULL",
			"past":       "o.end_at <= " + now,
			"upcoming":   "o.end_at > " + now,
		}[strings.ToLower(t.Value)]
		if col == "" {
			return "", fmt.Errorf("is: %q is not a state of an event (recurring, allday, private, proposed, cancelled, invitation, past, upcoming)", t.Value)
		}
		return wrap(col, negated), nil
	}
	return "", fmt.Errorf("%q is not a field of a calendar event (try: calendar, attendee, organizer, status, next, during, duration, has, is)", t.Field)
}

// addressOrSelf matches an address column, with me() as the user's own.
func (c *compiler) addressOrSelf(col string, t *Term) (string, error) {
	if !isSelfFunc(t.Value) {
		return c.stringPredicate(col, t), nil
	}
	if c.opt.SelfAddresses == nil {
		return "", fmt.Errorf("%s: me() is not available here", t.Field)
	}
	addrs, err := c.opt.SelfAddresses()
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "1=0", nil
	}
	ph := make([]string, len(addrs))
	for i, a := range addrs {
		ph[i] = c.arg(strings.ToLower(a))
	}
	return col + " IN (" + strings.Join(ph, ", ") + ")", nil
}

// parseAhead reads 7d, 2w, 3m, 12h, or a bare number of days.
func parseAhead(v string) (time.Duration, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return 0, fmt.Errorf("needs a length, such as 7d")
	}
	unit := v[len(v)-1]
	n, err := strconv.Atoi(strings.TrimRight(v, "hdwmy"))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a length (try 12h, 7d, 2w, 3m)", v)
	}
	day := 24 * time.Hour
	switch unit {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * day, nil
	case 'm':
		return time.Duration(n) * 30 * day, nil
	case 'y':
		return time.Duration(n) * 365 * day, nil
	default:
		return time.Duration(n) * day, nil
	}
}

// parseMoment reads a date (the whole day) or a date and time (that minute).
func parseMoment(v string) (int64, int64, error) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(v), time.Local); err == nil {
			return t.UnixMilli(), t.Add(time.Minute).UnixMilli(), nil
		}
	}
	return ParseDateValue(v)
}

// eventOrder is the order an event list reads in: soonest first.
func (p *pipeline) buildEvents() (*Plan, error) {
	where := "(" + p.where + ") AND " + eventBase
	// A proposal is shown on the calendar but is not yet time anybody
	// spends, so it is left out of lists and totals unless asked for by name.
	if !strings.Contains(p.where, "e.proposed = 1") {
		where += " AND e.proposed = 0"
	}
	args := append([]any{}, p.args...)
	if p.terminal != nil {
		switch p.terminal.Kind {
		case StageCount:
			if p.terminal.Field == "" {
				return &Plan{SQL: "SELECT COUNT(*)" + eventFrom + "WHERE " + where, Args: args, Kind: PlanScalar, Entity: EntityEvent}, nil
			}
			return p.buildEventGroups(where, args, p.terminal.Field, "COUNT(*)", p.clampLimit(200))
		case StageTop:
			return p.buildEventGroups(where, args, p.terminal.Field, "COUNT(*)", p.clampLimit(p.terminal.N))
		case StageAggregate:
			return p.buildEventAggregate(where, args)
		default:
			return nil, fmt.Errorf("%s does not apply to events (try: count, top, sum hours by week)", p.terminal.Kind)
		}
	}
	order := "o.start_at, o.end_at DESC"
	if p.sort != nil {
		switch strings.ToLower(p.sort.Field) {
		case "date", "start", "time", "":
			order = "o.start_at"
		case "end":
			order = "o.end_at"
		case "duration", "length":
			order = "(o.end_at - o.start_at)"
		case "title", "summary":
			order = "e.summary"
		case "calendar":
			order = "c.name"
		default:
			return nil, fmt.Errorf("cannot sort events by %q (try: date, end, duration, title, calendar)", p.sort.Field)
		}
		if p.sort.Desc {
			order += " DESC"
		} else {
			order += " ASC"
		}
	}
	limit := p.clampLimit(p.opt.defaultLimit())
	args = append(args, limit)
	sql := "SELECT " + EventSelectList + eventFrom + "WHERE " + where + " ORDER BY " + order + " LIMIT ?"
	if p.opt.Offset > 0 {
		sql += " OFFSET ?"
		args = append(args, p.opt.Offset)
	}
	return &Plan{SQL: sql, Args: args, Kind: PlanEvents, Limit: limit, Entity: EntityEvent}, nil
}

// eventGroupExpr is what an event query can be grouped by, and whether the
// grouping fans out over attendees.
func eventGroupExpr(field string) (expr string, joinAttendees bool, chronological bool, err error) {
	switch strings.ToLower(field) {
	case "calendar":
		return "c.name", false, false, nil
	case "organizer":
		return "COALESCE(NULLIF(e.organizer,''), '(none)')", false, false, nil
	case "attendee", "with":
		return "ea.address", true, false, nil
	case "domain":
		return "substr(ea.address, instr(ea.address, '@') + 1)", true, false, nil
	case "status":
		return "COALESCE(NULLIF(e.my_status,''), '(none)')", false, false, nil
	case "show":
		return "e.show_as", false, false, nil
	case "title", "summary":
		return "e.summary", false, false, nil
	case "location":
		return "COALESCE(NULLIF(e.location,''), '(none)')", false, false, nil
	case "charge":
		return "COALESCE(NULLIF(o.charge,''), '(not charged)')", false, false, nil
	case "hour", "day", "week", "month", "year":
		b, err := bucketExpr("o.start_at", strings.ToLower(field))
		return b, false, true, err
	}
	return "", false, false, fmt.Errorf("cannot group events by %q (try: calendar, attendee, domain, organizer, status, title, charge, day, week, month)", field)
}

func (p *pipeline) buildEventGroups(where string, args []any, field, value string, limit int) (*Plan, error) {
	expr, fan, chrono, err := eventGroupExpr(field)
	if err != nil {
		return nil, err
	}
	from := eventFrom
	if fan {
		from += "JOIN event_attendees ea ON ea.event_id = e.id "
	}
	order := "value DESC, label ASC"
	if chrono {
		order = "label ASC"
	}
	args = append(args, limit)
	sql := "SELECT " + expr + " AS label, " + value + " AS value" + from + "WHERE " + where +
		" GROUP BY label ORDER BY " + order + " LIMIT ?"
	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true, Entity: EntityEvent,
		GroupField: strings.ToLower(field), Bucket: bucketOf(field)}, nil
}

// buildEventAggregate is `| sum hours by week`: time, added up. The quantity
// is the occurrence's length, in hours or minutes.
func (p *pipeline) buildEventAggregate(where string, args []any) (*Plan, error) {
	unit := 3600000.0
	switch strings.ToLower(p.terminal.Field) {
	case "hours", "hour", "duration", "time":
	case "minutes", "minute", "mins":
		unit = 60000.0
	case "count", "events", "meetings":
		unit = 0
	default:
		return nil, fmt.Errorf("%s: an event has hours or minutes to add up, not %q (try: | sum hours by week)", p.terminal.Func, p.terminal.Field)
	}
	value := fmt.Sprintf("ROUND(%s((o.end_at - o.start_at) / %f), 2)", strings.ToUpper(p.terminal.Func), unit)
	if unit == 0 {
		value = "COUNT(*)"
	}
	if p.terminal.Bucket == "" {
		return &Plan{SQL: "SELECT COALESCE(" + value + ", 0)" + eventFrom + "WHERE " + where, Args: args,
			Kind: PlanScalar, Entity: EntityEvent}, nil
	}
	return p.buildEventGroups(where, args, p.terminal.Bucket, value, p.clampLimit(500))
}
