package calsync

import (
	"context"
	"testing"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/store"
)

type fake struct {
	events   map[string]RemoteEvent
	deleted  []string
	full     bool
	pushed   []string
	conflict bool
}

func (f *fake) Calendars(context.Context) ([]store.CalendarInfo, error) {
	return []store.CalendarInfo{{RemoteID: "primary", Name: "Work", Access: "owner", Primary: true}}, nil
}

func (f *fake) Pull(_ context.Context, _ store.CalendarInfo, token string) (*PullResult, error) {
	r := &PullResult{NextToken: "t1", Full: f.full || token == "", Deletes: f.deleted}
	for _, e := range f.events {
		r.Upserts = append(r.Upserts, e)
	}
	return r, nil
}

func (f *fake) Push(_ context.Context, _ store.CalendarInfo, ch store.CalendarChange, ev *store.StoredEvent) (*PushResult, error) {
	if f.conflict {
		return nil, ErrConflict
	}
	f.pushed = append(f.pushed, ch.Op)
	remote := "r-" + ev.UID
	f.events[remote] = RemoteEvent{Event: ev.Model(), RemoteID: remote, ETag: "e2"}
	return &PushResult{RemoteID: remote, ETag: "e2"}, nil
}

func setup(t *testing.T) (*Engine, *fake) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour).UTC()
	f := &fake{events: map[string]RemoteEvent{
		"r1": {RemoteID: "r1", ETag: "e1", Event: calendar.Event{UID: "u1", Summary: "Planning", Start: start, End: start.Add(time.Hour)}},
		"r2": {RemoteID: "r2", ETag: "e1", Event: calendar.Event{UID: "u2", Summary: "Review", Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)}},
	}}
	e := New()
	e.Register("test", func(store.CalendarAccount) (Provider, error) { return f, nil })
	store.SaveCalendarAccount(store.CalendarAccount{ID: "acct", Provider: "test", Name: "Work", Status: "ok"}, "")
	return e, f
}

func titles(t *testing.T) map[string]bool {
	occ, err := store.ListOccurrences(time.Now(), time.Now().Add(72*time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, o := range occ {
		out[o.Summary] = true
	}
	return out
}

func TestPullDeleteAndPrune(t *testing.T) {
	e, f := setup(t)
	e.Pass(context.Background())
	if got := titles(t); !got["Planning"] || !got["Review"] {
		t.Fatalf("after first pull %v", got)
	}
	cal, _ := store.CalendarByRemote("acct", "primary")
	if cal.SyncToken != "t1" || cal.Name != "Work" {
		t.Errorf("calendar %+v", cal)
	}
	// An incremental delete.
	delete(f.events, "r2")
	f.deleted = []string{"r2"}
	e.Pass(context.Background())
	if got := titles(t); got["Review"] {
		t.Errorf("deleted event still shown %v", got)
	}
	// A full resync drops what the provider no longer has.
	f.deleted, f.full = nil, true
	delete(f.events, "r1")
	e.Pass(context.Background())
	if got := titles(t); len(got) != 0 {
		t.Errorf("after full resync %v", got)
	}
}

func TestPushCreateAndConflict(t *testing.T) {
	e, f := setup(t)
	e.Pass(context.Background())
	cal, _ := store.CalendarByRemote("acct", "primary")
	start := time.Now().Add(30 * time.Hour).Truncate(time.Hour).UTC()
	id, _ := store.UpsertEvent(cal.ID, calendar.Event{UID: "new", Summary: "Lunch", Start: start, End: start.Add(time.Hour)}, store.EventMeta{})
	chg, _ := store.QueueCalendarChange(store.CalendarChange{CalendarID: cal.ID, EventID: id, Op: "create"})
	e.Pass(context.Background())
	if c, _ := store.GetCalendarChange(chg); c.State != "done" {
		t.Errorf("change state %q (%s)", c.State, c.Error)
	}
	ev, _ := store.GetEvent(id)
	if ev.RemoteID != "r-new" || ev.ETag != "e2" {
		t.Errorf("event after push %+v", ev)
	}
	// A conflict is not forced; the change is kept and marked.
	f.conflict = true
	chg2, _ := store.QueueCalendarChange(store.CalendarChange{CalendarID: cal.ID, EventID: id, Op: "update"})
	e.Pass(context.Background())
	if c, _ := store.GetCalendarChange(chg2); c.State != "conflict" {
		t.Errorf("conflict state %q", c.State)
	}
}

// An agent's change waits for a person; the engine does not send it.
func TestProposedChangesAreNotPushed(t *testing.T) {
	e, f := setup(t)
	e.Pass(context.Background())
	cal, _ := store.CalendarByRemote("acct", "primary")
	start := time.Now().Add(30 * time.Hour).Truncate(time.Hour).UTC()
	id, _ := store.UpsertEvent(cal.ID, calendar.Event{UID: "agent", Summary: "Agent idea", Start: start, End: start.Add(time.Hour)}, store.EventMeta{Origin: "agent", Proposed: true})
	store.QueueCalendarChange(store.CalendarChange{CalendarID: cal.ID, EventID: id, Op: "create", Origin: "agent"})
	e.Pass(context.Background())
	if len(f.pushed) != 0 {
		t.Errorf("pushed %v", f.pushed)
	}
}
