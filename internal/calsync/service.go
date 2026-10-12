package calsync

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/store"
)

// Changes made here, by a person or by an agent.
//
// # The rule
//
// A person's change happens: it is applied to the calendar at once and, for a
// calendar that lives at a provider, queued to go up on the next pass.
//
// An agent's change does not happen. It is recorded as a proposal — a new
// event shows, marked proposed, in the calendar it would go to; an edit,
// deletion or answer waits in the list of proposed changes — and nothing
// reaches the calendar or anyone else until a person approves it. The same
// shape as the outbox and proposed tickets.

// Origins.
const (
	OriginHuman = "human"
	OriginAgent = "agent"
)

// Scopes of an edit to a recurring event.
const (
	ScopeAll       = "all"
	ScopeThis      = "this"
	ScopeFollowing = "following"
)

// Payload is what a change carries: everything needed to apply it later.
type Payload struct {
	Event      *calendar.Event `json:"event,omitempty"`
	Scope      string          `json:"scope,omitempty"`
	Occurrence time.Time       `json:"occurrence,omitzero"`
	Status     string          `json:"status,omitempty"`
	Comment    string          `json:"comment,omitempty"`
	// Summary is a line for the proposals list: what this change does.
	Summary string `json:"summary,omitempty"`
}

// Result says what a change did.
type Result struct {
	EventID  string `json:"eventId,omitempty"`
	ChangeID string `json:"changeId,omitempty"`
	State    string `json:"state"` // applied, queued, proposed
	DraftID  string `json:"draftId,omitempty"`
	Note     string `json:"note,omitempty"`
}

// ErrReadOnly is a calendar that cannot be changed from here.
var ErrReadOnly = errors.New("this calendar cannot be changed from here")

// NewUID makes an event UID.
func NewUID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b) + "@inboxql"
}

func (e *Engine) nudge() {
	if e != nil {
		e.Kick()
	}
}

func remote(c *store.CalendarInfo) bool {
	return c.Provider == store.ProviderGoogle || c.Provider == store.ProviderEWS
}

func queue(c *store.CalendarInfo, eventID, op, origin string, p Payload) (string, error) {
	b, _ := json.Marshal(p)
	state := "pending"
	switch {
	case origin == OriginAgent:
		state = "proposed"
	case !remote(c):
		// Nothing to send; recorded so the change has a history.
		state = "done"
	}
	return store.QueueCalendarChange(store.CalendarChange{CalendarID: c.ID, EventID: eventID, Op: op,
		Payload: b, State: state, Origin: origin})
}

// Create adds an event to a calendar.
func (e *Engine) Create(calendarID string, ev calendar.Event, origin string) (*Result, error) {
	cal, err := store.GetCalendar(calendarID)
	if err != nil {
		return nil, fmt.Errorf("no calendar %q", calendarID)
	}
	if !cal.Writable() {
		return nil, ErrReadOnly
	}
	if err := validate(ev); err != nil {
		return nil, err
	}
	if ev.UID == "" {
		ev.UID = NewUID()
	}
	if ev.Status == "" {
		ev.Status = "confirmed"
	}
	agent := origin == OriginAgent
	id, err := store.UpsertEvent(cal.ID, ev, store.EventMeta{Origin: origin, Proposed: agent, MyStatus: "organizer"})
	if err != nil {
		return nil, err
	}
	chg, err := queue(cal, id, "create", origin, Payload{Event: &ev, Summary: "Create “" + ev.Summary + "”"})
	if err != nil {
		return nil, err
	}
	e.nudge()
	return &Result{EventID: id, ChangeID: chg, State: stateOf(origin, cal)}, nil
}

func stateOf(origin string, cal *store.CalendarInfo) string {
	switch {
	case origin == OriginAgent:
		return "proposed"
	case remote(cal):
		return "queued"
	}
	return "applied"
}

func validate(ev calendar.Event) error {
	if strings.TrimSpace(ev.Summary) == "" {
		return errors.New("an event needs a title")
	}
	if ev.Start.IsZero() || ev.End.IsZero() {
		return errors.New("an event needs a start and an end")
	}
	if ev.End.Before(ev.Start) {
		return errors.New("an event cannot end before it starts")
	}
	if ev.TZ != "" {
		if _, ok := calendar.LoadZone(ev.TZ); !ok {
			return fmt.Errorf("%q is not a time zone", ev.TZ)
		}
	}
	return nil
}

// Update changes an event: the whole series, one occurrence, or this one and
// the ones after it.
func (e *Engine) Update(eventID, scope string, occurrence time.Time, next calendar.Event, origin string) (*Result, error) {
	stored, err := store.GetEvent(eventID)
	if err != nil {
		return nil, fmt.Errorf("no event %q", eventID)
	}
	cal, err := store.GetCalendar(stored.CalendarID)
	if err != nil {
		return nil, err
	}
	if !cal.Writable() {
		return nil, ErrReadOnly
	}
	if err := validate(next); err != nil {
		return nil, err
	}
	if scope == "" {
		scope = ScopeAll
	}
	p := Payload{Event: &next, Scope: scope, Occurrence: occurrence.UTC(), Summary: "Change “" + stored.Summary + "”"}
	if origin == OriginAgent {
		chg, err := queue(cal, eventID, "update", origin, p)
		return &Result{EventID: eventID, ChangeID: chg, State: "proposed"}, err
	}
	id, err := applyUpdate(stored, scope, occurrence.UTC(), next)
	if err != nil {
		return nil, err
	}
	chg, err := queue(cal, id, "update", origin, p)
	if err != nil {
		return nil, err
	}
	if scope == ScopeFollowing {
		// The new series is a second event at the provider.
		if _, err := queue(cal, store.EventID(cal.ID, followingUID(stored.UID, occurrence), time.Time{}), "create", origin,
			Payload{Summary: "Create the rest of “" + stored.Summary + "”"}); err != nil {
			return nil, err
		}
	}
	e.nudge()
	return &Result{EventID: id, ChangeID: chg, State: stateOf(origin, cal)}, nil
}

func followingUID(uid string, occurrence time.Time) string {
	return fmt.Sprintf("%s-%s", uid, occurrence.UTC().Format("20060102T150405Z"))
}

var untilOrCount = regexp.MustCompile(`;?(UNTIL|COUNT)=[^;]*`)

// applyUpdate writes an edit and returns the id of the event that changed.
func applyUpdate(stored *store.StoredEvent, scope string, occurrence time.Time, next calendar.Event) (string, error) {
	master := stored
	if stored.RecurrenceID != nil {
		// Editing an override: "all" means the series it belongs to.
		if scope == ScopeAll {
			all, err := store.EventsByUID(stored.CalendarID, stored.UID)
			if err != nil {
				return "", err
			}
			for i := range all {
				if all[i].RecurrenceID == nil {
					master = &all[i]
				}
			}
		} else if occurrence.IsZero() {
			occurrence = *stored.RecurrenceID
		}
	}
	keep := func(ev *calendar.Event, from *store.StoredEvent) {
		ev.UID = from.UID
		ev.Organizer = orStr(ev.Organizer, from.Organizer)
		ev.Sequence = from.Sequence + 1
	}
	switch scope {
	case ScopeThis:
		if occurrence.IsZero() {
			return "", errors.New("say which occurrence to change")
		}
		next.RecurrenceID, next.Recurrence = occurrence, nil
		keep(&next, master)
		return store.UpsertEvent(master.CalendarID, next, store.EventMeta{Origin: OriginHuman})
	case ScopeFollowing:
		if occurrence.IsZero() || len(master.Recurrence) == 0 {
			return "", errors.New("“this and following” needs a recurring event and an occurrence")
		}
		// End the old series just before this occurrence…
		old := master.Model()
		until := occurrence.Add(-time.Second).UTC().Format("20060102T150405Z")
		for i, line := range old.Recurrence {
			if strings.HasPrefix(strings.ToUpper(line), "RRULE:") {
				old.Recurrence[i] = untilOrCount.ReplaceAllString(line, "") + ";UNTIL=" + until
			}
		}
		old.Sequence++
		if _, err := store.UpsertEvent(master.CalendarID, old, store.EventMeta{Origin: OriginHuman}); err != nil {
			return "", err
		}
		// …and start a new one at it, with what was left of the rule: an end
		// date carries over as it was; a count carries over as the
		// occurrences remaining, so the split series is no longer than the
		// original.
		next.UID = followingUID(master.UID, occurrence)
		if len(next.Recurrence) == 0 {
			remaining, counted := calendar.RemainingCount(master.Model(), occurrence)
			for _, line := range master.Recurrence {
				if !strings.HasPrefix(strings.ToUpper(line), "RRULE:") {
					continue
				}
				rule := line
				if counted {
					rule = untilOrCount.ReplaceAllString(line, "") + fmt.Sprintf(";COUNT=%d", remaining)
				}
				next.Recurrence = append(next.Recurrence, rule)
			}
		}
		next.Organizer = orStr(next.Organizer, master.Organizer)
		return store.UpsertEvent(master.CalendarID, next, store.EventMeta{Origin: OriginHuman, MyStatus: "organizer"})
	default:
		keep(&next, master)
		if master.RecurrenceID != nil {
			next.RecurrenceID = *master.RecurrenceID
		}
		return store.UpsertEvent(master.CalendarID, next, store.EventMeta{Origin: OriginHuman})
	}
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Delete removes an event, or one occurrence of a series.
func (e *Engine) Delete(eventID, scope string, occurrence time.Time, origin string) (*Result, error) {
	stored, err := store.GetEvent(eventID)
	if err != nil {
		return nil, fmt.Errorf("no event %q", eventID)
	}
	cal, err := store.GetCalendar(stored.CalendarID)
	if err != nil {
		return nil, err
	}
	if !cal.Writable() {
		return nil, ErrReadOnly
	}
	if scope == "" {
		scope = ScopeAll
	}
	p := Payload{Scope: scope, Occurrence: occurrence.UTC(), Summary: "Delete “" + stored.Summary + "”"}
	if origin == OriginAgent {
		chg, err := queue(cal, eventID, "delete", origin, p)
		return &Result{EventID: eventID, ChangeID: chg, State: "proposed"}, err
	}
	id, err := applyDelete(stored, cal, scope, occurrence.UTC())
	if err != nil {
		return nil, err
	}
	chg, err := queue(cal, id, "delete", origin, p)
	if err != nil {
		return nil, err
	}
	e.nudge()
	return &Result{EventID: id, ChangeID: chg, State: stateOf(origin, cal)}, nil
}

func applyDelete(stored *store.StoredEvent, cal *store.CalendarInfo, scope string, occurrence time.Time) (string, error) {
	if scope == ScopeThis && (len(stored.Recurrence) > 0 || stored.RecurrenceID != nil) {
		if occurrence.IsZero() && stored.RecurrenceID != nil {
			occurrence = *stored.RecurrenceID
		}
		if occurrence.IsZero() {
			return "", errors.New("say which occurrence to delete")
		}
		ov := stored.Model()
		ov.Recurrence, ov.RecurrenceID, ov.Status = nil, occurrence, "cancelled"
		ov.Sequence++
		return store.UpsertEvent(cal.ID, ov, store.EventMeta{Origin: OriginHuman})
	}
	if remote(cal) {
		// Kept, marked cancelled, until the provider has it: the push needs
		// the event's provider id, and an offline deletion must survive a
		// restart.
		ev := stored.Model()
		ev.Status = "cancelled"
		return store.UpsertEvent(cal.ID, ev, store.EventMeta{Origin: OriginHuman})
	}
	var rid time.Time
	if stored.RecurrenceID != nil {
		rid = *stored.RecurrenceID
	}
	return stored.ID, store.DeleteEvent(cal.ID, stored.UID, rid)
}

// Respond answers an invitation: accepted, tentative or declined.
//
// For an event in a Google or Exchange calendar the answer goes through the
// provider. For one that only arrived by mail, it goes back by mail — an
// iMIP REPLY, queued in the outbox like any message, for a person to approve
// with `iql outbox approve`.
func (e *Engine) Respond(eventID, status, comment, origin string) (*Result, error) {
	switch status {
	case "accepted", "tentative", "declined":
	default:
		return nil, fmt.Errorf("%q is not an answer (accepted, tentative, declined)", status)
	}
	stored, err := store.GetEvent(eventID)
	if err != nil {
		return nil, fmt.Errorf("no event %q", eventID)
	}
	cal, err := store.GetCalendar(stored.CalendarID)
	if err != nil {
		return nil, err
	}
	p := Payload{Status: status, Comment: comment, Summary: titleCase(status) + " “" + stored.Summary + "”"}
	if origin == OriginAgent {
		b, _ := json.Marshal(p)
		chg, err := store.QueueCalendarChange(store.CalendarChange{CalendarID: cal.ID, EventID: eventID, Op: "rsvp",
			Payload: b, State: "proposed", Origin: origin})
		return &Result{EventID: eventID, ChangeID: chg, State: "proposed"}, err
	}
	return e.applyRespond(stored, cal, p)
}

func (e *Engine) applyRespond(stored *store.StoredEvent, cal *store.CalendarInfo, p Payload) (*Result, error) {
	me, _ := store.MyAddresses()
	if err := store.SetMyStatus(stored.UID, p.Status, me); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(p)
	switch {
	case remote(cal):
		chg, err := store.QueueCalendarChange(store.CalendarChange{CalendarID: cal.ID, EventID: stored.ID, Op: "rsvp",
			Payload: b, State: "pending", Origin: OriginHuman})
		e.nudge()
		return &Result{EventID: stored.ID, ChangeID: chg, State: "queued"}, err
	case cal.Provider == store.ProviderMail && stored.Organizer != "":
		draftID, err := replyByMail(stored, me, p)
		if err != nil {
			return nil, err
		}
		chg, _ := store.QueueCalendarChange(store.CalendarChange{CalendarID: cal.ID, EventID: stored.ID, Op: "rsvp",
			Payload: b, State: "done", Origin: OriginHuman})
		return &Result{EventID: stored.ID, ChangeID: chg, State: "queued", DraftID: draftID,
			Note: "The answer is queued as mail to " + stored.Organizer + ". Nothing is sent until you run: iql outbox approve " + draftID}, nil
	default:
		return &Result{EventID: stored.ID, State: "applied"}, nil
	}
}

// replyByMail queues the iMIP REPLY to the organiser, from the account the
// invitation arrived on.
func replyByMail(stored *store.StoredEvent, me []string, p Payload) (string, error) {
	mine := ""
	for _, a := range stored.Attendees {
		for _, m := range me {
			if strings.EqualFold(a.Address, m) {
				mine = a.Address
			}
		}
	}
	if mine == "" {
		return "", errors.New("you are not on this invitation's list of attendees, so there is nothing to answer as")
	}
	ics, err := calendar.Reply(stored.Model(), mine, p.Status)
	if err != nil {
		return "", err
	}
	accountID, inReplyTo := "", ""
	if stored.SourceMessageID != "" {
		if m, err := store.GetMessageByID(stored.SourceMessageID); err == nil && m != nil {
			accountID, inReplyTo = m.AccountID, m.MessageID
		}
	}
	if accountID == "" {
		return "", errors.New("cannot tell which account the invitation arrived on, to answer from")
	}
	word := map[string]string{"accepted": "Accepted", "tentative": "Tentative", "declined": "Declined"}[p.Status]
	body := word + ": " + stored.Summary
	if p.Comment != "" {
		body += "\n\n" + p.Comment
	}
	now := time.Now()
	d := &store.Draft{ID: "draft_" + strings.Split(NewUID(), "@")[0], AccountID: accountID, InReplyTo: inReplyTo,
		To: []string{stored.Organizer}, Subject: word + ": " + stored.Summary, Body: body,
		Status: store.DraftStatusQueued, Origin: store.OriginHuman, QueuedAt: &now}
	if err := store.SaveDraft(d); err != nil {
		return "", err
	}
	return d.ID, store.SetDraftCalendar(d.ID, "REPLY", string(ics))
}

// Approve turns an agent's proposal into a change, as if a person had made
// it. Callers must have established that a person asked for this.
func (e *Engine) Approve(changeID string) (*Result, error) {
	ch, err := store.GetCalendarChange(changeID)
	if err != nil {
		return nil, fmt.Errorf("no change %q", changeID)
	}
	if ch.State != "proposed" {
		return nil, fmt.Errorf("change %s is %s, not proposed", changeID, ch.State)
	}
	var p Payload
	_ = json.Unmarshal(ch.Payload, &p)
	cal, err := store.GetCalendar(ch.CalendarID)
	if err != nil {
		return nil, err
	}
	res := &Result{ChangeID: changeID, EventID: ch.EventID}
	switch ch.Op {
	case "create":
		stored, err := store.GetEvent(ch.EventID)
		if err != nil {
			return nil, err
		}
		if _, err := store.UpsertEvent(cal.ID, stored.Model(), store.EventMeta{Origin: OriginAgent, Proposed: false}); err != nil {
			return nil, err
		}
	case "update":
		stored, err := store.GetEvent(ch.EventID)
		if err != nil || p.Event == nil {
			return nil, errors.New("the event this change was for is gone")
		}
		if res.EventID, err = applyUpdate(stored, p.Scope, p.Occurrence, *p.Event); err != nil {
			return nil, err
		}
	case "delete":
		stored, err := store.GetEvent(ch.EventID)
		if err != nil {
			return nil, errors.New("the event this change was for is gone")
		}
		if res.EventID, err = applyDelete(stored, cal, p.Scope, p.Occurrence); err != nil {
			return nil, err
		}
	case "rsvp":
		stored, err := store.GetEvent(ch.EventID)
		if err != nil {
			return nil, errors.New("the event this change was for is gone")
		}
		if err := store.SetCalendarChangeState(changeID, "done", ""); err != nil {
			return nil, err
		}
		return e.applyRespond(stored, cal, p)
	}
	next := "done"
	if remote(cal) {
		next = "pending"
	}
	if err := store.SetCalendarChangeState(changeID, next, ""); err != nil {
		return nil, err
	}
	e.nudge()
	res.State = map[string]string{"done": "applied", "pending": "queued"}[next]
	return res, nil
}

// Reject refuses an agent's proposal. A proposed new event is removed; the
// rejection is kept, as evidence of what was suggested and refused.
func (e *Engine) Reject(changeID string) error {
	ch, err := store.GetCalendarChange(changeID)
	if err != nil {
		return fmt.Errorf("no change %q", changeID)
	}
	if ch.State != "proposed" {
		return fmt.Errorf("change %s is %s, not proposed", changeID, ch.State)
	}
	if ch.Op == "create" && ch.EventID != "" {
		if ev, err := store.GetEvent(ch.EventID); err == nil && ev.Proposed {
			_ = store.DeleteEvent(ev.CalendarID, ev.UID, time.Time{})
		}
	}
	return store.SetCalendarChangeState(changeID, "rejected", "")
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
