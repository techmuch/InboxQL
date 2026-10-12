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
}

// Text is how a reminder reads in a notification.
func (r Reminder) Text() string {
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
	return due, rows.Err()
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
