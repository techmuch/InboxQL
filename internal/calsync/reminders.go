package calsync

import (
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/user/inboxql/internal/store"
)

// Reminders.
//
// The server checks for alarms that have come due and hands each to the open
// windows, which show it and — with the person's permission — raise a
// browser notification. With no window open, the operating system is asked
// to show one instead.
//
// A machine that slept through its reminders does not wake to a burst of
// them: only alarms that came due in the last ten minutes are delivered, and
// the rest are logged as missed.

// Reminder is one alarm, come due.
type Reminder struct {
	EventID       string    `json:"eventId"`
	Summary       string    `json:"summary"`
	Start         time.Time `json:"start"`
	AllDay        bool      `json:"allDay"`
	Location      string    `json:"location,omitempty"`
	ConferenceURL string    `json:"conferenceUrl,omitempty"`
	Minutes       int       `json:"minutes"`
	// Note, when set, is how the reminder reads instead of a time: an
	// important date says "in 7 days", not a clock time.
	Note string `json:"note,omitempty"`
}

// Text is how a reminder reads in a notification.
func (r Reminder) Text() string {
	if r.Note != "" {
		return r.Summary + " — " + r.Note
	}
	when := "now"
	switch {
	case r.AllDay:
		when = "today"
	case r.Minutes >= 1440:
		when = r.Start.In(time.Local).Format("Mon 15:04")
	case r.Minutes > 0:
		when = "in " + strconv.Itoa(r.Minutes) + " min, at " + r.Start.In(time.Local).Format("15:04")
	}
	s := r.Summary + " — " + when
	if r.Location != "" {
		s += " · " + r.Location
	}
	return s
}

// Deliver hands a reminder to the windows; it reports whether any took it.
type Deliver func(Reminder) bool

const reminderCursorKey = "calendar_reminder_cursor"

// LateLimit is how late a reminder may still be delivered.
var LateLimit = 10 * time.Minute

// DueReminders returns alarms that came due in (after, until], skipping any
// later than LateLimit before until.
func DueReminders(after, until time.Time) ([]Reminder, error) {
	if after.Before(until.Add(-LateLimit)) {
		after = until.Add(-LateLimit)
	}
	// An alarm can be up to a week before its event; look that far ahead.
	rows, err := store.DB().Query(`SELECT o.event_id, e.summary, o.start_at, o.all_day, e.location, e.conference_url, e.alarms
		FROM event_occurrences o JOIN events e ON e.id = o.event_id JOIN calendars c ON c.id = e.calendar_id
		WHERE e.alarms != '[]' AND e.status != 'cancelled' AND e.proposed = 0 AND e.my_status != 'declined'
		AND c.hidden = 0 AND o.start_at > ? AND o.start_at <= ?`,
		after.Add(-time.Hour).UnixMilli(), until.Add(7*24*time.Hour).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var due []Reminder
	for rows.Next() {
		var r Reminder
		var start int64
		var alarms string
		if err := rows.Scan(&r.EventID, &r.Summary, &start, &r.AllDay, &r.Location, &r.ConferenceURL, &alarms); err != nil {
			return nil, err
		}
		r.Start = time.UnixMilli(start)
		var mins []int
		_ = json.Unmarshal([]byte(alarms), &mins)
		for _, m := range mins {
			at := r.Start.Add(-time.Duration(m) * time.Minute)
			if at.After(after) && !at.After(until) {
				rr := r
				rr.Minutes = m
				due = append(due, rr)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	dates, err := dueImportantDates(after, until)
	if err != nil {
		return nil, err
	}
	return append(due, dates...), nil
}

// ReminderHour is when an important date's reminders arrive, local time: a
// reminder about a date is for the morning, not the moment the day turns.
var ReminderHour = 8

// dueImportantDates returns reminders for marked dates that came due in
// (after, until]: when a lead time starts, a week before, the day before and
// the morning of. A suggestion nobody accepted reminds nobody.
func dueImportantDates(after, until time.Time) ([]Reminder, error) {
	ds, err := store.ListImportantDates()
	if err != nil {
		return nil, err
	}
	var due []Reminder
	for _, d := range ds {
		if d.Origin != "human" {
			continue
		}
		// The occurrence the window is approaching: from the start of the
		// window, so a yearly date that is today is still today's.
		day, ok := d.NextOccurrence(after.In(time.Local))
		if !ok {
			continue
		}
		stages := []struct {
			days int
			note string
		}{{0, "today"}, {1, "tomorrow"}, {7, "in 7 days"}}
		if d.LeadDays > 1 && d.LeadDays != 7 {
			stages = append(stages, struct {
				days int
				note string
			}{d.LeadDays, "in " + strconv.Itoa(d.LeadDays) + " days — time to start"})
		}
		for _, st := range stages {
			at := time.Date(day.Year(), day.Month(), day.Day()-st.days, ReminderHour, 0, 0, 0, time.Local)
			if at.After(after) && !at.After(until) {
				due = append(due, Reminder{
					EventID: "date:" + d.ID + ":" + strconv.Itoa(st.days),
					Summary: d.Title,
					Start:   day,
					AllDay:  true,
					Minutes: st.days * 1440,
					Note:    st.note,
				})
			}
		}
	}
	return due, nil
}

// RunReminders checks for due alarms until ctx ends.
func RunReminders(ctx context.Context, deliver Deliver) {
	cursor := time.Now()
	if v, _ := store.GetSetting(reminderCursorKey); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			cursor = time.UnixMilli(ms)
		}
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		now := time.Now()
		if now.Sub(cursor) > LateLimit {
			log.Printf("[calendar] reminders between %s and %s were missed (asleep or not running)",
				cursor.Format("15:04"), now.Add(-LateLimit).Format("15:04"))
		}
		due, err := DueReminders(cursor, now)
		if err != nil {
			log.Printf("[calendar] reminders: %v", err)
		}
		for _, r := range due {
			if !deliver(r) {
				notifyOS(r)
			}
		}
		cursor = now
		_ = store.UpdateSetting(reminderCursorKey, strconv.FormatInt(now.UnixMilli(), 10))
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// notifyOS shows a reminder with the operating system's own notifications,
// for when no InboxQL window is open to show it.
func notifyOS(r Reminder) {
	title, text := "InboxQL", r.Text()
	switch runtime.GOOS {
	case "darwin":
		script := `display notification "` + appleQuote(text) + `" with title "` + appleQuote(title) + `" sound name "default"`
		_ = exec.Command("osascript", "-e", script).Run()
	case "linux":
		_ = exec.Command("notify-send", "--app-name=InboxQL", title, text).Run()
	default:
		log.Printf("[calendar] reminder (no window open to show it): %s", text)
	}
}

func appleQuote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
