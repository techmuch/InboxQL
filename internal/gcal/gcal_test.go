package gcal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
)

// fakeGoogle is enough of Google Calendar to sync against.
type fakeGoogle struct {
	mu        sync.Mutex
	events    map[string]gEvent // by id
	etag      int
	refreshes int
	puts      []string
	patches   []map[string]any
	syncGone  bool
}

func (f *fakeGoogle) nextETag() string { f.etag++; return `"e` + string(rune('0'+f.etag)) + `"` }

func (f *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case path == "/token":
		r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			f.refreshes++
			if r.Form.Get("refresh_token") != "rt" {
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
		}
		w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
	case r.Header.Get("Authorization") != "Bearer at":
		w.WriteHeader(401)
	case path == "/api/users/me/calendarList":
		w.Write([]byte(`{"items":[
			{"id":"me@gmail.com","summary":"me@gmail.com","backgroundColor":"#16a765","timeZone":"America/New_York","accessRole":"owner","primary":true},
			{"id":"holidays","summary":"Holidays","accessRole":"reader","timeZone":"UTC"},
			{"id":"boss","summary":"Boss","accessRole":"freeBusyReader"}]}`))
	case strings.HasSuffix(path, "/instances"):
		master := strings.TrimSuffix(strings.TrimPrefix(path, "/api/calendars/me@gmail.com/events/"), "/instances")
		orig := r.URL.Query().Get("originalStart")
		w.Write([]byte(`{"items":[{"id":"` + master + `_inst","etag":"\"i1\"","originalStartTime":{"dateTime":"` + orig + `"}}]}`))
	case strings.HasPrefix(path, "/api/calendars/me@gmail.com/events"):
		id := strings.TrimPrefix(strings.TrimPrefix(path, "/api/calendars/me@gmail.com/events"), "/")
		switch r.Method {
		case http.MethodGet:
			if id != "" {
				json.NewEncoder(w).Encode(f.events[id])
				return
			}
			if f.syncGone && r.URL.Query().Get("syncToken") != "" {
				w.WriteHeader(http.StatusGone)
				return
			}
			var items []gEvent
			for _, e := range f.events {
				items = append(items, e)
			}
			// In a fixed order, as Google pages are: a map's order changes
			// between calls, and two pages built from it can overlap.
			sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
			// Two pages, the token on the last.
			if r.URL.Query().Get("pageToken") == "" && len(items) > 1 {
				json.NewEncoder(w).Encode(map[string]any{"items": items[:1], "nextPageToken": "p2"})
				return
			}
			if r.URL.Query().Get("pageToken") == "p2" {
				items = items[1:]
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items, "nextSyncToken": "s1"})
		case http.MethodPost:
			var g gEvent
			json.NewDecoder(r.Body).Decode(&g)
			g.ID, g.ETag, g.Status = "new1", f.nextETag(), "confirmed"
			f.events[g.ID] = g
			json.NewEncoder(w).Encode(g)
		case http.MethodPut:
			if cur, ok := f.events[id]; ok && r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != cur.ETag {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			var g gEvent
			json.NewDecoder(r.Body).Decode(&g)
			g.ID, g.ETag = id, f.nextETag()
			f.puts = append(f.puts, id)
			if !strings.HasSuffix(id, "_inst") {
				f.events[id] = g
			}
			json.NewEncoder(w).Encode(g)
		case http.MethodPatch:
			var m map[string]any
			json.NewDecoder(r.Body).Decode(&m)
			f.patches = append(f.patches, m)
			g := f.events[id]
			json.NewEncoder(w).Encode(g)
		case http.MethodDelete:
			delete(f.events, id)
			w.WriteHeader(204)
		}
	case strings.HasPrefix(path, "/api/calendars/") && r.Method == http.MethodGet:
		w.Write([]byte(`{"items":[],"nextSyncToken":"other"}`))
	default:
		w.WriteHeader(404)
	}
}

func setup(t *testing.T) (*fakeGoogle, *calsync.Engine) {
	t.Helper()
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour).UTC()
	f := &fakeGoogle{events: map[string]gEvent{
		"standup": {ID: "standup", ICalUID: "standup@google.com", ETag: `"s"`, Status: "confirmed", Summary: "Standup",
			Start:      &gTime{DateTime: start.Format(time.RFC3339), TimeZone: "America/New_York"},
			End:        &gTime{DateTime: start.Add(15 * time.Minute).Format(time.RFC3339), TimeZone: "America/New_York"},
			Recurrence: []string{"RRULE:FREQ=DAILY;COUNT=5", "EXDATE;TZID=America/New_York:" + start.In(mustLoc("America/New_York")).Add(24*time.Hour).Format("20060102T150405")},
			Organizer: &struct {
				Email string `json:"email"`
				Self  bool   `json:"self"`
			}{"me@gmail.com", true},
			HangoutLink: "https://meet.google.com/abc-defg-hij"},
		// The third day's standup, cancelled.
		"standup_3": {ID: "standup_3", ICalUID: "standup@google.com", Status: "cancelled", RecurringEventID: "standup",
			OriginalStartTime: &gTime{DateTime: start.Add(48 * time.Hour).Format(time.RFC3339)}},
		"invite": {ID: "invite", ICalUID: "invite@acme.com", ETag: `"v"`, Status: "confirmed", Summary: "Acme review",
			Start: &gTime{DateTime: start.Add(3 * time.Hour).Format(time.RFC3339)}, End: &gTime{DateTime: start.Add(4 * time.Hour).Format(time.RFC3339)},
			Attendees: []gAttendee{{Email: "me@gmail.com", Self: true, ResponseStatus: "needsAction"}, {Email: "ada@acme.com", ResponseStatus: "accepted"}}},
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	TokenURL, APIBase = srv.URL+"/token", srv.URL+"/api"
	store.SaveCalendarAccount(store.CalendarAccount{ID: "g", Provider: "google", Name: "me@gmail.com", Email: "me@gmail.com",
		Status: "ok", Config: map[string]string{"clientId": "cid", "clientSecret": "cs"}}, "rt")
	e := calsync.New()
	e.Register("google", New)
	return f, e
}

func mustLoc(n string) *time.Location { l, _ := time.LoadLocation(n); return l }

func summaries(t *testing.T) []string {
	occ, _ := store.ListOccurrences(time.Now(), time.Now().Add(8*24*time.Hour), nil)
	var out []string
	for _, o := range occ {
		out = append(out, o.Summary)
	}
	return out
}

func TestGoogleSync(t *testing.T) {
	f, e := setup(t)
	if err := e.SyncAccount(context.Background(), "g"); err != nil {
		t.Fatal(err)
	}
	cals, _ := store.ListCalendars()
	names := map[string]store.CalendarInfo{}
	for _, c := range cals {
		names[c.Name] = c
	}
	if _, ok := names["Boss"]; ok {
		t.Error("a free/busy-only calendar was listed")
	}
	if c := names["me@gmail.com"]; !c.Writable() || !c.Primary || c.SyncToken != "s1" {
		t.Errorf("primary calendar %+v", c)
	}
	if c := names["Holidays"]; c.Writable() {
		t.Error("a read-only calendar is writable")
	}
	// Five daily standups, one excluded by EXDATE and one cancelled: three,
	// and the invitation.
	got := summaries(t)
	n := 0
	for _, s := range got {
		if s == "Standup" {
			n++
		}
	}
	if n != 3 || !strings.Contains(strings.Join(got, ","), "Acme review") {
		t.Errorf("occurrences %v", got)
	}
	evs, _ := store.EventsByUID("", "invite@acme.com")
	if len(evs) != 1 || evs[0].MyStatus != "needs-action" || evs[0].RemoteID != "invite" {
		t.Errorf("invitation %+v", evs)
	}
	master, _ := store.EventsByUID("", "standup@google.com")
	for _, m := range master {
		if m.RecurrenceID == nil && (m.ConferenceURL != "https://meet.google.com/abc-defg-hij" || m.MyStatus != "organizer" || m.TZ != "America/New_York") {
			t.Errorf("standup %+v", m)
		}
	}
	if f.refreshes != 1 {
		t.Errorf("refreshed the token %d times", f.refreshes)
	}

	// An expired sync token falls back to a full sync.
	f.syncGone = true
	if err := e.SyncAccount(context.Background(), "g"); err != nil {
		t.Fatalf("after 410: %v", err)
	}
}

func TestGooglePush(t *testing.T) {
	f, e := setup(t)
	ctx := context.Background()
	e.SyncAccount(ctx, "g")
	primary, _ := store.CalendarByRemote("g", "me@gmail.com")

	// Create.
	start := time.Now().Add(72 * time.Hour).Truncate(time.Hour).UTC()
	r, err := e.Create(primary.ID, calendar.Event{Summary: "Lunch", Start: start, End: start.Add(time.Hour), TZ: "Europe/London"}, calsync.OriginHuman)
	if err != nil || r.State != "queued" {
		t.Fatalf("%+v %v", r, err)
	}
	e.SyncAccount(ctx, "g")
	ev, _ := store.GetEvent(r.EventID)
	if ev.RemoteID != "new1" {
		t.Errorf("created event remote id %q", ev.RemoteID)
	}
	if got := f.events["new1"]; got.Start.TimeZone != "Europe/London" || got.ICalUID == "" {
		t.Errorf("sent %+v", got)
	}

	// Move one standup: found through instances, not a guessed id.
	var standup *store.StoredEvent
	ms, _ := store.EventsByUID(primary.ID, "standup@google.com")
	for i := range ms {
		if ms[i].RecurrenceID == nil {
			standup = &ms[i]
		}
	}
	occ := standup.Start
	if _, err := e.Update(standup.ID, calsync.ScopeThis, occ, calendar.Event{Summary: "Standup (late)", Start: occ.Add(time.Hour), End: occ.Add(75 * time.Minute)}, calsync.OriginHuman); err != nil {
		t.Fatal(err)
	}
	e.SyncAccount(ctx, "g")
	if len(f.puts) == 0 || f.puts[len(f.puts)-1] != "standup_inst" {
		t.Errorf("puts %v", f.puts)
	}

	// Answer the invitation: the whole attendee list goes back, with only
	// your own answer changed.
	inv, _ := store.EventsByUID(primary.ID, "invite@acme.com")
	if _, err := e.Respond(inv[0].ID, "accepted", "", calsync.OriginHuman); err != nil {
		t.Fatal(err)
	}
	e.SyncAccount(ctx, "g")
	if len(f.patches) != 1 {
		t.Fatalf("patches %v", f.patches)
	}
	atts := f.patches[0]["attendees"].([]any)
	if len(atts) != 2 || atts[0].(map[string]any)["responseStatus"] != "accepted" || atts[1].(map[string]any)["responseStatus"] != "accepted" {
		t.Errorf("rsvp sent %v", atts)
	}

	// Somebody else changed it first: the change is not forced.
	f.events["new1"] = func() gEvent { g := f.events["new1"]; g.ETag = `"changed-elsewhere"`; return g }()
	ev, _ = store.GetEvent(r.EventID)
	u, _ := e.Update(ev.ID, calsync.ScopeAll, time.Time{}, calendar.Event{Summary: "Long lunch", Start: start, End: start.Add(2 * time.Hour)}, calsync.OriginHuman)
	e.SyncAccount(ctx, "g")
	if c, _ := store.GetCalendarChange(u.ChangeID); c.State != "conflict" {
		t.Errorf("change state %q", c.State)
	}

	// Delete.
	if _, err := e.Delete(r.EventID, calsync.ScopeAll, time.Time{}, calsync.OriginHuman); err != nil {
		t.Fatal(err)
	}
	ev, _ = store.GetEvent(r.EventID)
	// Forget the conflict's ETag so the delete is not refused.
	f.events["new1"] = func() gEvent { g := f.events["new1"]; g.ETag = ev.ETag; return g }()
	e.SyncAccount(ctx, "g")
	if _, ok := f.events["new1"]; ok {
		t.Error("not deleted at Google")
	}
	if _, err := store.GetEvent(r.EventID); err == nil {
		t.Error("still here after deletion")
	}
}

func TestRevokedSignInIsSaidPlainly(t *testing.T) {
	_, e := setup(t)
	store.SaveCalendarAccount(store.CalendarAccount{ID: "g", Provider: "google", Name: "me", Status: "ok",
		Config: map[string]string{"clientId": "cid"}}, "revoked")
	err := e.SyncAccount(context.Background(), "g")
	if err == nil || !strings.Contains(err.Error(), "connect the account again") {
		t.Errorf("err %v", err)
	}
	a, _ := store.GetCalendarAccount("g")
	if a.Status != "error" {
		t.Errorf("status %q", a.Status)
	}
}
