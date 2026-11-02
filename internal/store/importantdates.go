package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// # Important dates
//
// A date the person marked important: a deadline, a trip, an expiry, an
// anniversary. Today and Ahead count them down, and reminders arrive as one
// approaches — when its lead time starts, a week before, the day before and
// that morning.
//
// Only a person marks a date important. An agent may suggest one, and a
// suggestion is a row with origin "agent" that is not counted down until the
// person accepts it, which turns it into theirs.

// ImportantDate is one marked date.
type ImportantDate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Date is the day, YYYY-MM-DD, in the person's own calendar.
	Date string `json:"date"`
	// LeadDays is how long before the date there is something to do: renewing
	// a passport is six weeks, not the day it expires.
	LeadDays int `json:"leadDays"`
	// Yearly repeats it every year on the same day, for anniversaries.
	Yearly bool `json:"yearly"`
	// RefKind and RefID say what it is about: "event", "ticket", "message",
	// or nothing.
	RefKind string `json:"refKind,omitempty"`
	RefID   string `json:"refId,omitempty"`
	Note    string `json:"note,omitempty"`
	// Origin is "human", or "agent" for a suggestion not yet accepted.
	Origin    string `json:"origin"`
	CreatedAt int64  `json:"createdAt"`
}

// validate checks the fields a person could get wrong, and tidies them.
func (d *ImportantDate) validate() error {
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" {
		return fmt.Errorf("an important date needs a title")
	}
	if _, err := time.Parse("2006-01-02", d.Date); err != nil {
		return fmt.Errorf("date %q is not YYYY-MM-DD", d.Date)
	}
	if d.LeadDays < 0 || d.LeadDays > 366 {
		return fmt.Errorf("a lead time is between 0 and 366 days, not %d", d.LeadDays)
	}
	switch d.RefKind {
	case "", "event", "ticket", "message":
	default:
		return fmt.Errorf("an important date is about an event, a ticket or a message, not %q", d.RefKind)
	}
	if d.Origin == "" {
		d.Origin = "human"
	}
	if d.Origin != "human" && d.Origin != "agent" {
		return fmt.Errorf("origin is human or agent, not %q", d.Origin)
	}
	return nil
}

// ListImportantDates returns every marked date, soonest first by its date.
func ListImportantDates() ([]ImportantDate, error) {
	rows, err := db.Query(`SELECT id, title, date, lead_days, yearly, ref_kind, ref_id, note, origin, created_at
		FROM important_dates ORDER BY date, title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportantDate{}
	for rows.Next() {
		var d ImportantDate
		var yearly int
		if err := rows.Scan(&d.ID, &d.Title, &d.Date, &d.LeadDays, &yearly, &d.RefKind, &d.RefID, &d.Note, &d.Origin, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Yearly = yearly != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveImportantDate adds a date, or replaces the one with the same id.
func SaveImportantDate(d *ImportantDate) error {
	if err := d.validate(); err != nil {
		return err
	}
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	if d.CreatedAt == 0 {
		d.CreatedAt = time.Now().UnixMilli()
	}
	yearly := 0
	if d.Yearly {
		yearly = 1
	}
	_, err := db.Exec(`INSERT INTO important_dates (id, title, date, lead_days, yearly, ref_kind, ref_id, note, origin, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET title = excluded.title, date = excluded.date, lead_days = excluded.lead_days,
			yearly = excluded.yearly, ref_kind = excluded.ref_kind, ref_id = excluded.ref_id, note = excluded.note,
			origin = excluded.origin`,
		d.ID, d.Title, d.Date, d.LeadDays, yearly, d.RefKind, d.RefID, d.Note, d.Origin, d.CreatedAt)
	return err
}

// DeleteImportantDate removes a date. A date that is not there is not an error:
// the outcome asked for already holds.
func DeleteImportantDate(id string) error {
	_, err := db.Exec(`DELETE FROM important_dates WHERE id = ?`, id)
	return err
}

// NextOccurrence is the date's next day on or after from: itself, or for a
// yearly date this year's or next year's. ok is false for a one-off date that
// has passed.
func (d ImportantDate) NextOccurrence(from time.Time) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", d.Date, from.Location())
	if err != nil {
		return time.Time{}, false
	}
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	if !d.Yearly {
		return t, !t.Before(day)
	}
	next := time.Date(day.Year(), t.Month(), t.Day(), 0, 0, 0, 0, from.Location())
	if next.Before(day) {
		next = next.AddDate(1, 0, 0)
	}
	return next, true
}
