package cli

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/user/inboxql/internal/api"
	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "calendar",
		Aliases: []string{"cal"},
		Summary: "read and change your calendars",
		Usage: `iql calendar <command> [flags]

  agenda       what is on: --next 7d (default), or --from/--to
  calendars    the calendars, and which accounts they come from
  show <id>    one event, with its attendees and the message it came from
  create       add an event (see flags below)
  update <id>  change an event; --scope this|following|all, --occurrence for one
  delete <id>  remove an event, or one occurrence with --scope this --occurrence
  respond <id> accepted|tentative|declined [--comment text]
  changes      changes waiting: proposed, going up, in conflict, failed
  approve <id> turn a proposed change into a real one — at a terminal only
  reject <id>  refuse a proposed change
  import <file.ics> [--calendar id]
  export --calendar <id> [--out file.ics]
  invitations  read invitations from mail now (--rescan reads all mail again)

create / update flags:
  --calendar <id>      default: your own calendar
  --title <text>       --description <text>   --location <text>
  --start <time>       --end <time>           --duration 30m
  --all-day            --tz <zone>            e.g. Europe/London
  --attendee <addr>    repeatable
  --repeat <rule>      an RRULE, e.g. FREQ=WEEKLY;BYDAY=MO,WE
  --reminder <mins>    repeatable
  --origin agent       an agent's change: recorded as a proposal

Times are "2026-10-15 14:00", "2026-10-15T14:00", RFC 3339, or a date for an
all-day event, read in --tz or this machine's zone.

An agent's changes are proposals. They show on the calendar, marked, and
reach nobody until a person approves them — here, at a terminal, or in the
Calendar tab. Answering an invitation that only arrived by mail queues the
answer as mail, sent like any other with ` + "`iql outbox approve`" + `.

The same events are a query: ` + "`iql query \"in:events next:7d\"`" + `.`,
		Run: runCalendar,
	})
}

type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

func runCalendar(ctx *Context, args []string) error {
	if len(args) == 0 {
		args = []string{"agenda"}
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	e := newCalendarEngine()
	sub, rest := args[0], args[1:]
	switch sub {
	case "agenda", "list":
		return calendarAgenda(ctx, rest)
	case "calendars":
		return calendarCalendars(ctx)
	case "show":
		return calendarShow(ctx, rest)
	case "create", "add", "new":
		return calendarCreate(ctx, e, rest)
	case "update", "edit":
		return calendarUpdate(ctx, e, rest)
	case "delete", "remove":
		return calendarDelete(ctx, e, rest)
	case "respond", "rsvp":
		return calendarRespond(ctx, e, rest)
	case "changes", "proposals":
		return calendarChanges(ctx)
	case "approve":
		return calendarApprove(ctx, e, rest)
	case "reject":
		return calendarReject(ctx, e, rest)
	case "import":
		return calendarImport(ctx, rest)
	case "export":
		return calendarExport(ctx, rest)
	case "invitations":
		return calendarInvitations(ctx, rest)
	case "accounts", "connect", "sync", "disconnect":
		return calendarAccounts(ctx, e, sub, rest)
	}
	return Fail(ExitUsage, "unknown calendar command %q (see `iql help calendar`)", sub)
}

func parseWhen(v, tz string) (time.Time, bool, error) {
	loc := time.Local
	if tz != "" {
		l, ok := calendar.LoadZone(tz)
		if !ok {
			return time.Time{}, false, fmt.Errorf("%q is not a time zone", tz)
		}
		loc = l
	}
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, false, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 3:04pm", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.UTC); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf("%q is not a time (try \"2026-10-15 14:00\")", v)
}

func calendarAgenda(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("agenda", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	next := fs.String("next", "7d", "how far ahead")
	from := fs.String("from", "", "start")
	to := fs.String("to", "", "end")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	start, end := time.Now(), time.Time{}
	if *from != "" {
		t, _, err := parseWhen(*from, "")
		if err != nil {
			return Fail(ExitUsage, "--from: %v", err)
		}
		start = t
	}
	if *to != "" {
		t, _, err := parseWhen(*to, "")
		if err != nil {
			return Fail(ExitUsage, "--to: %v", err)
		}
		end = t
	} else {
		d, err := time.ParseDuration(strings.Replace(strings.Replace(*next, "d", "h", 1), "w", "h", 1))
		if strings.HasSuffix(*next, "d") {
			d *= 24
		} else if strings.HasSuffix(*next, "w") {
			d *= 24 * 7
		}
		if err != nil {
			return Fail(ExitUsage, "--next: %q is not a length (7d, 2w, 12h)", *next)
		}
		end = start.Add(d)
	}
	occ, err := store.ListOccurrences(start, end, nil)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"from": start, "to": end, "count": len(occ), "occurrences": occ})
	}
	p := ctx.Printer()
	if len(occ) == 0 {
		ctx.Printf("Nothing on between %s and %s.\n", start.Format("Mon 2 Jan 15:04"), end.Format("Mon 2 Jan 15:04"))
		return nil
	}
	day := ""
	for _, o := range occ {
		local := o.Start.In(time.Local)
		if d := local.Format("Monday 2 January"); d != day {
			day = d
			ctx.Printf("\n%s\n", p.Bold(d))
		}
		when := local.Format("15:04") + "–" + o.End.In(time.Local).Format("15:04")
		if o.AllDay {
			when = "all day    "
		}
		mark := ""
		switch {
		case o.Proposed:
			mark = p.Yellow(" (proposed)")
		case o.MyStatus == "needs-action":
			mark = p.Yellow(" (not answered)")
		case o.MyStatus == "tentative":
			mark = p.Dim(" (tentative)")
		}
		ctx.Printf("  %s  %s%s  %s\n", p.Dim(when), o.Summary, mark, p.Dim(o.EventID))
	}
	return nil
}

func calendarCalendars(ctx *Context) error {
	cals, err := store.ListCalendars()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"calendars": cals})
	}
	p := ctx.Printer()
	for _, c := range cals {
		rw := "read-only"
		if c.Writable() {
			rw = "writable"
		}
		ctx.Printf("%-22s %-14s %-9s %5d events  %s\n", c.ID, c.Provider, rw, c.Events, p.Bold(c.Name))
	}
	return nil
}

func requireEvent(id string) (*store.StoredEvent, error) {
	ev, err := store.GetEvent(id)
	if err != nil {
		return nil, Fail(ExitNotFound, "no event %q (ids are in `iql calendar agenda`)", id)
	}
	return ev, nil
}

func calendarShow(ctx *Context, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql calendar show <event-id>")
	}
	ev, err := requireEvent(args[0])
	if err != nil {
		return err
	}
	if ctx.JSON {
		return ctx.EmitJSON(ev)
	}
	p := ctx.Printer()
	ctx.Printf("%s\n", p.Bold(ev.Summary))
	when := ev.Start.In(time.Local).Format("Mon 2 Jan 2006 15:04") + " – " + ev.End.In(time.Local).Format("15:04")
	if ev.AllDay {
		when = ev.Start.Format("Mon 2 Jan 2006") + ", all day"
	}
	ctx.Printf("  %-10s %s\n", p.Dim("when"), when)
	if ev.Repeats != "" {
		ctx.Printf("  %-10s %s\n", p.Dim("repeats"), ev.Repeats)
	}
	for _, kv := range [][2]string{{"where", ev.Location}, {"join", ev.ConferenceURL}, {"organizer", ev.Organizer},
		{"you", ev.MyStatus}, {"calendar", ev.CalendarID}} {
		if kv[1] != "" {
			ctx.Printf("  %-10s %s\n", p.Dim(kv[0]), kv[1])
		}
	}
	for _, a := range ev.Attendees {
		ctx.Printf("  %-10s %s %s\n", p.Dim("attendee"), a.Address, p.Dim("("+a.Status+")"))
	}
	if ev.SourceMessageID != "" {
		ctx.Printf("  %-10s iql read %s\n", p.Dim("message"), ev.SourceMessageID)
	}
	if ev.Description != "" {
		ctx.Printf("\n%s\n", ev.Description)
	}
	return nil
}

// eventFlags reads the create/update flags into an event, starting from base.
func eventFlags(ctx *Context, name string, args []string, base calendar.Event) (calendar.Event, string, map[string]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	cal := fs.String("calendar", store.LocalCalendarID, "calendar")
	title := fs.String("title", "", "title")
	desc := fs.String("description", "", "description")
	loc := fs.String("location", "", "location")
	startS := fs.String("start", "", "start")
	endS := fs.String("end", "", "end")
	dur := fs.Duration("duration", 0, "length")
	allDay := fs.Bool("all-day", false, "all day")
	tz := fs.String("tz", "", "time zone")
	repeat := fs.String("repeat", "", "RRULE")
	origin := fs.String("origin", "human", "who is making this change")
	scope := fs.String("scope", "all", "this, following or all")
	occurrence := fs.String("occurrence", "", "which occurrence")
	var attendees, reminders repeated
	fs.Var(&attendees, "attendee", "attendee")
	fs.Var(&reminders, "reminder", "minutes before")
	if err := parseArgs(fs, args); err != nil {
		return base, "", nil, Fail(ExitUsage, "invalid flags")
	}
	ev := base
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["title"] {
		ev.Summary = *title
	}
	if set["description"] {
		ev.Description = *desc
	}
	if set["location"] {
		ev.Location = *loc
	}
	if set["tz"] {
		ev.TZ = *tz
	}
	if set["start"] {
		t, day, err := parseWhen(*startS, ev.TZ)
		if err != nil {
			return ev, "", nil, Fail(ExitUsage, "--start: %v", err)
		}
		length := ev.End.Sub(ev.Start)
		ev.Start, ev.AllDay = t, day || *allDay
		if !set["end"] && !set["duration"] {
			if length <= 0 {
				length = time.Hour
				if ev.AllDay {
					length = 24 * time.Hour
				}
			}
			ev.End = ev.Start.Add(length)
		}
	}
	if set["end"] {
		t, _, err := parseWhen(*endS, ev.TZ)
		if err != nil {
			return ev, "", nil, Fail(ExitUsage, "--end: %v", err)
		}
		ev.End = t
	}
	if set["duration"] {
		ev.End = ev.Start.Add(*dur)
	}
	if set["all-day"] {
		ev.AllDay = *allDay
	}
	if set["repeat"] {
		ev.Recurrence = nil
		if r := strings.TrimPrefix(strings.TrimSpace(*repeat), "RRULE:"); r != "" {
			ev.Recurrence = []string{"RRULE:" + r}
		}
	}
	if set["attendee"] {
		ev.Attendees = nil
		for _, a := range attendees {
			ev.Attendees = append(ev.Attendees, calendar.Attendee{Address: strings.ToLower(a), Role: "required", Status: "needs-action"})
		}
	}
	if set["reminder"] {
		ev.Alarms = nil
		for _, r := range reminders {
			var m int
			fmt.Sscan(r, &m)
			ev.Alarms = append(ev.Alarms, m)
		}
	}
	return ev, *origin, map[string]string{"calendar": *cal, "scope": *scope, "occurrence": *occurrence}, nil
}

func originOf(o string) (string, error) {
	switch o {
	case "", "human":
		return calsync.OriginHuman, nil
	case "agent":
		return calsync.OriginAgent, nil
	}
	return "", Fail(ExitUsage, "--origin is human or agent")
}

func reportResult(ctx *Context, verb string, r *calsync.Result) error {
	if ctx.JSON {
		return ctx.EmitJSON(r)
	}
	switch r.State {
	case "proposed":
		ctx.Printf("Proposed: %s. Nothing changes until a person approves it:\n    iql calendar approve %s\n", verb, r.ChangeID)
	case "queued":
		ctx.Printf("%s. It goes to the provider on the next sync.\n", strings.ToUpper(verb[:1])+verb[1:])
	default:
		ctx.Printf("%s.\n", strings.ToUpper(verb[:1])+verb[1:])
	}
	if r.Note != "" {
		ctx.Printf("%s\n", r.Note)
	}
	if r.EventID != "" && !ctx.JSON {
		ctx.Printf("%s\n", ctx.Printer().Dim(r.EventID))
	}
	return nil
}

func calendarCreate(ctx *Context, e *calsync.Engine, args []string) error {
	ev, o, opts, err := eventFlags(ctx, "create", args, calendar.Event{})
	if err != nil {
		return err
	}
	origin, err := originOf(o)
	if err != nil {
		return err
	}
	if ev.Start.IsZero() {
		return Fail(ExitUsage, "--start is required")
	}
	r, err := e.Create(opts["calendar"], ev, origin)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	return reportResult(ctx, "created “"+ev.Summary+"”", r)
}

func splitID(args []string, usage string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, Fail(ExitUsage, "usage: %s", usage)
	}
	return args[0], args[1:], nil
}

func calendarUpdate(ctx *Context, e *calsync.Engine, args []string) error {
	id, rest, err := splitID(args, "iql calendar update <event-id> [flags]")
	if err != nil {
		return err
	}
	stored, err := requireEvent(id)
	if err != nil {
		return err
	}
	ev, o, opts, err := eventFlags(ctx, "update", rest, stored.Model())
	if err != nil {
		return err
	}
	origin, err := originOf(o)
	if err != nil {
		return err
	}
	var occ time.Time
	if opts["occurrence"] != "" {
		if occ, _, err = parseWhen(opts["occurrence"], ""); err != nil {
			return Fail(ExitUsage, "--occurrence: %v", err)
		}
	}
	r, err := e.Update(id, opts["scope"], occ, ev, origin)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	return reportResult(ctx, "changed “"+ev.Summary+"”", r)
}

func calendarDelete(ctx *Context, e *calsync.Engine, args []string) error {
	id, rest, err := splitID(args, "iql calendar delete <event-id> [--scope this --occurrence <time>]")
	if err != nil {
		return err
	}
	stored, err := requireEvent(id)
	if err != nil {
		return err
	}
	_, o, opts, err := eventFlags(ctx, "delete", rest, stored.Model())
	if err != nil {
		return err
	}
	origin, err := originOf(o)
	if err != nil {
		return err
	}
	var occ time.Time
	if opts["occurrence"] != "" {
		if occ, _, err = parseWhen(opts["occurrence"], ""); err != nil {
			return Fail(ExitUsage, "--occurrence: %v", err)
		}
	}
	r, err := e.Delete(id, opts["scope"], occ, origin)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	return reportResult(ctx, "deleted “"+stored.Summary+"”", r)
}

func calendarRespond(ctx *Context, e *calsync.Engine, args []string) error {
	fs := flag.NewFlagSet("respond", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	comment := fs.String("comment", "", "a note to the organizer")
	o := fs.String("origin", "human", "who is answering")
	if err := parseArgs(fs, args); err != nil || fs.NArg() != 2 {
		return Fail(ExitUsage, "usage: iql calendar respond <event-id> accepted|tentative|declined [--comment text]")
	}
	origin, err := originOf(*o)
	if err != nil {
		return err
	}
	if _, err := requireEvent(fs.Arg(0)); err != nil {
		return err
	}
	r, err := e.Respond(fs.Arg(0), fs.Arg(1), *comment, origin)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	return reportResult(ctx, fs.Arg(1), r)
}

func calendarChanges(ctx *Context) error {
	changes, err := store.ListCalendarChanges("proposed", "pending", "conflict", "failed")
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		if changes == nil {
			changes = []store.CalendarChange{}
		}
		return ctx.EmitJSON(map[string]any{"changes": changes})
	}
	if len(changes) == 0 {
		ctx.Printf("Nothing waiting.\n")
		return nil
	}
	p := ctx.Printer()
	for _, c := range changes {
		var pl calsync.Payload
		_ = jsonUnmarshal(c.Payload, &pl)
		what := pl.Summary
		if what == "" {
			what = c.Op
		}
		ctx.Printf("%-22s %-9s %-6s %s", c.ID, c.State, c.Origin, what)
		if c.Error != "" {
			ctx.Printf("  %s", p.Dim(c.Error))
		}
		ctx.Printf("\n")
	}
	return nil
}

// calendarApprove is the gate. An agent's proposal becomes real only when a
// person says so, at a terminal — the same rule as `iql outbox approve`, and
// for the same reason: a flag would be available to the agent it exists to
// stop.
func calendarApprove(ctx *Context, e *calsync.Engine, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql calendar approve <change-id>")
	}
	if !IsTerminal() {
		return Fail(ExitNeedsApprove,
			"approval requires a terminal.\n\nThis change stays proposed. A person must run:\n"+
				"    iql calendar approve %s\nor approve it in the Calendar tab. There is no flag to bypass this.", args[0])
	}
	ch, err := store.GetCalendarChange(args[0])
	if err != nil {
		return Fail(ExitNotFound, "no change %q (see `iql calendar changes`)", args[0])
	}
	var pl calsync.Payload
	_ = jsonUnmarshal(ch.Payload, &pl)
	ctx.Printf("%s\nproposed by %s, %s\n\nApprove? Type yes: ", pl.Summary, ch.Origin, ch.CreatedAt.Format("Mon 2 Jan 15:04"))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(strings.ToLower(line)) != "yes" {
		ctx.Printf("Not approved; it stays proposed.\n")
		return nil
	}
	r, err := e.Approve(args[0])
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	return reportResult(ctx, "approved", r)
}

func calendarReject(ctx *Context, e *calsync.Engine, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql calendar reject <change-id>")
	}
	if err := e.Reject(args[0]); err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"rejected": args[0]})
	}
	ctx.Printf("Rejected; kept as a record of what was proposed.\n")
	return nil
}

func calendarImport(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	target := fs.String("calendar", store.LocalCalendarID, "calendar")
	if err := parseArgs(fs, args); err != nil || fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: iql calendar import <file.ics> [--calendar id]")
	}
	cal, err := store.GetCalendar(*target)
	if err != nil || (cal.Provider != store.ProviderLocal && cal.Provider != store.ProviderICS) {
		return Fail(ExitUsage, "import into a calendar of your own, not one that syncs elsewhere")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	n, err := api.ImportICS(cal.ID, data)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"imported": n, "calendar": cal.ID})
	}
	ctx.Printf("Imported %d events into %s.\n", n, cal.Name)
	return nil
}

func calendarExport(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	target := fs.String("calendar", store.LocalCalendarID, "calendar")
	out := fs.String("out", "", "file to write")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	data, err := api.ExportICS(*target)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if *out == "" {
		_, err = ctx.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return Fail(ExitError, "%v", err)
	}
	ctx.Printf("Wrote %s.\n", *out)
	return nil
}

func calendarInvitations(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("invitations", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	rescan := fs.Bool("rescan", false, "read every message again")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if *rescan {
		if err := store.ResetMailInvitations(); err != nil {
			return Fail(ExitError, "%v", err)
		}
	}
	rep, err := store.ProcessMailInvitations(0)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(rep)
	}
	ctx.Printf("Read %d messages carrying invitations: %d events, %d cancellations, %d replies, %d older updates ignored.\n",
		rep.Messages, rep.Events, rep.Cancelled, rep.Replies, rep.Stale)
	return nil
}
