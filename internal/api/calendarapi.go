package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/uisession"
)

// The calendar: calendars and accounts, occurrences for the views, events,
// changes, invitations, import and export.
//
// Everything here acts as a person: the interface is a person at a screen.
// An agent works through `iql calendar … --origin agent`, whose changes are
// proposals; approving one is the one thing here that asks the request to
// have come from the page itself.

var calendarEngine *calsync.Engine

// SetCalendarEngine gives the API the running calendar engine.
func SetCalendarEngine(e *calsync.Engine) { calendarEngine = e }

func engine() *calsync.Engine {
	if calendarEngine == nil {
		calendarEngine = calsync.New()
	}
	return calendarEngine
}

func registerCalendarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/calendar/calendars", handleCalendars)
	mux.HandleFunc("PATCH /api/calendar/calendars/{id}", handleCalendarPatch)
	mux.HandleFunc("GET /api/calendar/occurrences", handleOccurrences)
	mux.HandleFunc("POST /api/calendar/events", handleEventCreate)
	mux.HandleFunc("GET /api/calendar/events/{id}", handleEventGet)
	mux.HandleFunc("PUT /api/calendar/events/{id}", handleEventUpdate)
	mux.HandleFunc("DELETE /api/calendar/events/{id}", handleEventDelete)
	mux.HandleFunc("POST /api/calendar/events/{id}/respond", handleEventRespond)
	mux.HandleFunc("GET /api/calendar/changes", handleChanges)
	mux.HandleFunc("POST /api/calendar/changes/{id}/approve", handleChangeApprove)
	mux.HandleFunc("POST /api/calendar/changes/{id}/reject", handleChangeReject)
	mux.HandleFunc("GET /api/calendar/invitation", handleInvitationForMessage)
	mux.HandleFunc("POST /api/calendar/invitations/rescan", handleInvitationRescan)
	mux.HandleFunc("POST /api/calendar/import", handleCalendarImport)
	mux.HandleFunc("GET /api/calendar/export", handleCalendarExport)
	mux.HandleFunc("GET /api/calendar/accounts", handleCalendarAccounts)
	mux.HandleFunc("POST /api/calendar/accounts/{id}/sync", handleCalendarAccountSync)
	mux.HandleFunc("DELETE /api/calendar/accounts/{id}", handleCalendarAccountDelete)
	registerCalendarAccountRoutes(mux)
}

// EventInput is an event as the interface sends it.
type EventInput struct {
	Summary       string              `json:"summary"`
	Description   string              `json:"description"`
	Location      string              `json:"location"`
	ConferenceURL string              `json:"conferenceUrl"`
	Start         time.Time           `json:"start"`
	End           time.Time           `json:"end"`
	AllDay        bool                `json:"allDay"`
	TZ            string              `json:"tz"`
	Recurrence    []string            `json:"recurrence"`
	ShowAs        string              `json:"showAs"`
	Private       bool                `json:"private"`
	Attendees     []calendar.Attendee `json:"attendees"`
	Alarms        []int               `json:"alarms"`
}

func (in EventInput) model() calendar.Event {
	ev := calendar.Event{Summary: strings.TrimSpace(in.Summary), Description: in.Description, Location: in.Location,
		ConferenceURL: in.ConferenceURL, Start: in.Start.UTC(), End: in.End.UTC(), AllDay: in.AllDay, TZ: in.TZ,
		Recurrence: in.Recurrence, ShowAs: in.ShowAs, Private: in.Private, Alarms: in.Alarms}
	if ev.AllDay {
		// An all-day event is dates, not instants: midnight UTC of each.
		ev.Start = time.Date(in.Start.Year(), in.Start.Month(), in.Start.Day(), 0, 0, 0, 0, time.UTC)
		ev.End = time.Date(in.End.Year(), in.End.Month(), in.End.Day(), 0, 0, 0, 0, time.UTC)
		if !ev.End.After(ev.Start) {
			ev.End = ev.Start.AddDate(0, 0, 1)
		}
		ev.TZ = ""
	}
	for _, a := range in.Attendees {
		a.Address = strings.ToLower(strings.TrimSpace(a.Address))
		if a.Address == "" {
			continue
		}
		if a.Role == "" {
			a.Role = "required"
		}
		if a.Status == "" {
			a.Status = "needs-action"
		}
		ev.Attendees = append(ev.Attendees, a)
	}
	return ev
}

func calendarError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, calsync.ErrReadOnly):
		writeError(w, http.StatusForbidden, "%v", err)
	case strings.HasPrefix(err.Error(), "no "):
		writeError(w, http.StatusNotFound, "%v", err)
	default:
		writeError(w, http.StatusBadRequest, "%v", err)
	}
}

func handleCalendars(w http.ResponseWriter, r *http.Request) {
	cals, err := store.ListCalendars()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	accts, _ := store.ListCalendarAccounts()
	out := make([]map[string]any, 0, len(cals))
	for _, c := range cals {
		out = append(out, map[string]any{"id": c.ID, "accountId": c.AccountID, "provider": c.Provider, "name": c.Name,
			"color": c.Color, "timeZone": c.TimeZone, "access": c.Access, "hidden": c.Hidden, "primary": c.Primary,
			"writable": c.Writable(), "events": c.Events, "syncedAt": c.SyncedAt})
	}
	pending, _ := store.ListCalendarChanges("proposed", "conflict", "failed")
	writeJSON(w, http.StatusOK, map[string]any{"calendars": out, "accounts": accts, "attention": len(pending)})
}

func handleCalendarPatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hidden *bool   `json:"hidden"`
		Color  *string `json:"color"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if body.Hidden != nil {
		if err := store.SetCalendarHidden(id, *body.Hidden); err != nil {
			writeError(w, http.StatusNotFound, "no calendar %q", id)
			return
		}
	}
	if body.Color != nil {
		store.SetCalendarColor(id, *body.Color)
	}
	c, _ := store.GetCalendar(id)
	writeJSON(w, http.StatusOK, c)
}

func parseRange(r *http.Request) (time.Time, time.Time, error) {
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from: %v", err)
	}
	to, err := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("to: %v", err)
	}
	if to.Sub(from) > 400*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("ask for at most a year and a bit at a time")
	}
	return from, to, nil
}

func handleOccurrences(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	var ids []string
	if c := r.URL.Query().Get("calendars"); c != "" {
		ids = strings.Split(c, ",")
	}
	occ, err := store.ListOccurrences(from, to, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"occurrences": occ})
}

func handleEventCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CalendarID string     `json:"calendarId"`
		Event      EventInput `json:"event"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	if body.CalendarID == "" {
		body.CalendarID = store.LocalCalendarID
	}
	res, err := engine().Create(body.CalendarID, body.Event.model(), calsync.OriginHuman)
	if err != nil {
		calendarError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// handleEventGet is the event card: the event, how it repeats, its calendar,
// the message it came from, and any changes waiting on it.
func handleEventGet(w http.ResponseWriter, r *http.Request) {
	ev, err := store.GetEvent(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no event %q", r.PathValue("id"))
		return
	}
	cal, _ := store.GetCalendar(ev.CalendarID)
	out := map[string]any{"event": ev, "calendar": cal}
	if cal != nil {
		out["writable"] = cal.Writable()
	}
	if ev.SourceMessageID != "" {
		if m, err := store.GetMessageByID(ev.SourceMessageID); err == nil && m != nil {
			out["message"] = map[string]any{"id": m.ID, "subject": m.Subject, "from": m.From, "date": m.Date}
		}
	}
	changes, _ := store.ListCalendarChanges("proposed", "pending", "conflict", "failed")
	mine := []store.CalendarChange{}
	for _, c := range changes {
		if c.EventID == ev.ID {
			mine = append(mine, c)
		}
	}
	out["changes"] = mine
	me, _ := store.MyAddresses()
	out["me"] = me
	writeJSON(w, http.StatusOK, out)
}

func occurrenceParam(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, v)
}

func handleEventUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope      string     `json:"scope"`
		Occurrence string     `json:"occurrence"`
		Event      EventInput `json:"event"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	occ, err := occurrenceParam(body.Occurrence)
	if err != nil {
		writeError(w, http.StatusBadRequest, "occurrence: %v", err)
		return
	}
	res, err := engine().Update(r.PathValue("id"), body.Scope, occ, body.Event.model(), calsync.OriginHuman)
	if err != nil {
		calendarError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func handleEventDelete(w http.ResponseWriter, r *http.Request) {
	occ, err := occurrenceParam(r.URL.Query().Get("occurrence"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "occurrence: %v", err)
		return
	}
	res, err := engine().Delete(r.PathValue("id"), r.URL.Query().Get("scope"), occ, calsync.OriginHuman)
	if err != nil {
		calendarError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func handleEventRespond(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	res, err := engine().Respond(r.PathValue("id"), body.Status, body.Comment, calsync.OriginHuman)
	if err != nil {
		calendarError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func handleChanges(w http.ResponseWriter, r *http.Request) {
	states := []string{"proposed", "pending", "conflict", "failed"}
	if s := r.URL.Query().Get("state"); s != "" {
		states = strings.Split(s, ",")
	}
	changes, err := store.ListCalendarChanges(states...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if changes == nil {
		changes = []store.CalendarChange{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changes})
}

// fromThePage reports whether a request came from the web page, as opposed to
// a script. Browsers mark every request with Sec-Fetch-Site and cannot be
// made to leave it off; curl and the CLI send nothing. It is not proof
// against something that forges the header — it is the line AGENTS.md draws,
// written down where it is enforced.
func fromThePage(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func handleChangeApprove(w http.ResponseWriter, r *http.Request) {
	if !fromThePage(r) {
		writeError(w, http.StatusForbidden,
			"a proposed change is approved by a person, in the Calendar tab or with `iql calendar approve` at a terminal")
		return
	}
	res, err := engine().Approve(r.PathValue("id"))
	if err != nil {
		calendarError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func handleChangeReject(w http.ResponseWriter, r *http.Request) {
	if err := engine().Reject(r.PathValue("id")); err != nil {
		calendarError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rejected": r.PathValue("id")})
}

// handleInvitationForMessage is the invitation card in the message viewer.
func handleInvitationForMessage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("message")
	evs, err := store.EventsForMessage(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if len(evs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"events": []any{}})
		return
	}
	// What else is on at the same time — the question an invitation asks.
	out := []map[string]any{}
	for _, ev := range evs {
		clashes := []store.OccurrenceView{}
		if occ, err := store.ListOccurrences(ev.Start, ev.End, nil); err == nil {
			for _, o := range occ {
				if o.UID != ev.UID && o.ShowAs != "free" {
					clashes = append(clashes, o)
				}
			}
		}
		out = append(out, map[string]any{"event": ev, "clashes": clashes})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func handleInvitationRescan(w http.ResponseWriter, r *http.Request) {
	if err := store.ResetMailInvitations(); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	engine().Kick()
	writeJSON(w, http.StatusAccepted, map[string]any{"rescanning": true})
}

// handleCalendarImport reads an .ics file into a calendar this program owns.
func handleCalendarImport(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("calendar")
	if target == "" {
		target = store.LocalCalendarID
	}
	cal, err := store.GetCalendar(target)
	if err != nil || (cal.Provider != store.ProviderLocal && cal.Provider != store.ProviderICS) {
		writeError(w, http.StatusBadRequest, "import into a calendar of your own, not one that syncs elsewhere")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	n, err := ImportICS(cal.ID, data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": n, "calendar": cal.ID})
}

// ImportICS stores every event in an iCalendar file in a calendar.
func ImportICS(calendarID string, data []byte) (int, error) {
	cal, err := calendar.Parse(data)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, ev := range cal.Events {
		if _, err := store.UpsertEvent(calendarID, ev, store.EventMeta{Origin: "import"}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func handleCalendarExport(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("calendar")
	cal, err := store.GetCalendar(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "no calendar %q", id)
		return
	}
	data, err := ExportICS(cal.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.ics"`, strings.ReplaceAll(cal.Name, `"`, "")))
	w.Write(data)
}

// ExportICS writes a calendar's events — every series, override and single
// event — as one iCalendar file.
func ExportICS(calendarID string) ([]byte, error) {
	stored, err := store.EventsInCalendar(calendarID)
	if err != nil {
		return nil, err
	}
	events := make([]calendar.Event, 0, len(stored))
	for _, e := range stored {
		events = append(events, e.Model())
	}
	return calendar.Encode("", events)
}

func handleCalendarAccounts(w http.ResponseWriter, r *http.Request) {
	accts, err := store.ListCalendarAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if accts == nil {
		accts = []store.CalendarAccount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accts})
}

func handleCalendarAccountSync(w http.ResponseWriter, r *http.Request) {
	if err := engine().SyncAccount(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadGateway, "%v", err)
		return
	}
	a, _ := store.GetCalendarAccount(r.PathValue("id"))
	writeJSON(w, http.StatusOK, a)
}

// handleCalendarAccountDelete forgets an account and its calendars here.
// Nothing at the provider is touched.
func handleCalendarAccountDelete(w http.ResponseWriter, r *http.Request) {
	if err := store.DeleteCalendarAccount(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": r.PathValue("id")})
}

// DeliverReminder hands a reminder to every connected window, and reports
// whether any took it. With none, the caller asks the operating system.
func DeliverReminder(r calsync.Reminder) bool {
	b, _ := json.Marshal(r)
	delivered := false
	for _, s := range sessions.List() {
		if !s.Connected {
			continue
		}
		if err := sessions.Send(s.Name, uisession.Command{Verb: "reminder", Arg: string(b), Note: ""}); err == nil {
			delivered = true
		}
	}
	return delivered
}
