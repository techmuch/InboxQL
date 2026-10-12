package ews

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
)

// fakeExchange is enough of EWS to sync against. Every request is parsed as
// XML, so a malformed one fails the test rather than passing it.
type fakeExchange struct {
	mu        sync.Mutex
	t         *testing.T
	items     map[string]string // id -> ics
	keys      map[string]string // id -> change key
	synced    bool
	requests  []string
	created   []string
	accepted  []string
	deleted   []string
	updatedID []string
}

const soapOK = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"
 xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types"><s:Body>%s</s:Body></s:Envelope>`

var createOrder = []string{"Subject", "Sensitivity", "Body", "ReminderIsSet", "ReminderMinutesBeforeStart", "UID", "Start", "End",
	"IsAllDayEvent", "LegacyFreeBusyStatus", "Location", "RequiredAttendees", "OptionalAttendees", "Recurrence", "StartTimeZone", "EndTimeZone"}

func (f *fakeExchange) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, ok := r.BasicAuth(); !ok || u != `CORP\me` || p != "pw" {
		w.WriteHeader(401)
		return
	}
	b, _ := io.ReadAll(r.Body)
	body := string(b)
	if err := xml.Unmarshal(b, new(struct{})); err != nil {
		f.t.Errorf("malformed request: %v\n%s", err, body)
	}
	f.requests = append(f.requests, body)
	reply := func(s string) { fmt.Fprintf(w, soapOK, s) }
	switch {
	case strings.Contains(body, "<m:GetFolder>"):
		reply(`<m:GetFolderResponse><m:ResponseMessages><m:GetFolderResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode>
<m:Folders><t:CalendarFolder><t:FolderId Id="cal-root" ChangeKey="x"/><t:DisplayName>Calendar</t:DisplayName></t:CalendarFolder></m:Folders>
</m:GetFolderResponseMessage></m:ResponseMessages></m:GetFolderResponse>`)
	case strings.Contains(body, "<m:FindFolder"):
		reply(`<m:FindFolderResponse><m:ResponseMessages><m:FindFolderResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode>
<m:RootFolder><t:Folders><t:CalendarFolder><t:FolderId Id="cal-team" ChangeKey="y"/><t:DisplayName>Team</t:DisplayName></t:CalendarFolder></t:Folders></m:RootFolder>
</m:FindFolderResponseMessage></m:ResponseMessages></m:FindFolderResponse>`)
	case strings.Contains(body, "<m:SyncFolderItems>"):
		var changes string
		if strings.Contains(body, `Id="cal-root"`) && !strings.Contains(body, "<m:SyncState>") {
			for id := range f.items {
				changes += `<t:Create><t:CalendarItem><t:ItemId Id="` + id + `" ChangeKey="` + f.keys[id] + `"/></t:CalendarItem></t:Create>`
			}
		}
		if strings.Contains(body, "<m:SyncState>gone</m:SyncState>") {
			reply(`<m:SyncFolderItemsResponse><m:ResponseMessages><m:SyncFolderItemsResponseMessage ResponseClass="Error"><m:MessageText>bad</m:MessageText><m:ResponseCode>ErrorInvalidSyncStateData</m:ResponseCode></m:SyncFolderItemsResponseMessage></m:ResponseMessages></m:SyncFolderItemsResponse>`)
			return
		}
		reply(`<m:SyncFolderItemsResponse><m:ResponseMessages><m:SyncFolderItemsResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode>
<m:SyncState>state-1</m:SyncState><m:IncludesLastItemInRange>true</m:IncludesLastItemInRange><m:Changes>` + changes + `</m:Changes>
</m:SyncFolderItemsResponseMessage></m:ResponseMessages></m:SyncFolderItemsResponse>`)
	case strings.Contains(body, "<m:GetItem>"):
		var items string
		for _, m := range regexp.MustCompile(`ItemId Id="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			id := m[1]
			resp := "NoResponseReceived"
			if id == "own" {
				resp = "Organizer"
			}
			items += `<t:CalendarItem><t:MimeContent CharacterSet="UTF-8">` + base64.StdEncoding.EncodeToString([]byte(f.items[id])) +
				`</t:MimeContent><t:ItemId Id="` + id + `" ChangeKey="` + f.keys[id] + `"/><t:MyResponseType>` + resp + `</t:MyResponseType><t:IsCancelled>false</t:IsCancelled></t:CalendarItem>`
		}
		reply(`<m:GetItemResponse><m:ResponseMessages><m:GetItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Items>` + items +
			`</m:Items></m:GetItemResponseMessage></m:ResponseMessages></m:GetItemResponse>`)
	case strings.Contains(body, "<t:AcceptItem>"):
		f.accepted = append(f.accepted, regexp.MustCompile(`ReferenceItemId Id="([^"]+)"`).FindStringSubmatch(body)[1])
		reply(`<m:CreateItemResponse><m:ResponseMessages><m:CreateItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode></m:CreateItemResponseMessage></m:ResponseMessages></m:CreateItemResponse>`)
	case strings.Contains(body, "<m:CreateItem"):
		// The fields must be in schema order.
		pos := -1
		for _, name := range createOrder {
			if i := strings.Index(body, "<t:"+name); i >= 0 {
				if i < pos {
					f.t.Errorf("%s is out of schema order", name)
				}
				pos = i
			}
		}
		f.created = append(f.created, body)
		reply(`<m:CreateItemResponse><m:ResponseMessages><m:CreateItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode>
<m:Items><t:CalendarItem><t:ItemId Id="made-1" ChangeKey="k1"/></t:CalendarItem></m:Items></m:CreateItemResponseMessage></m:ResponseMessages></m:CreateItemResponse>`)
	case strings.Contains(body, "<m:FindItem"):
		reply(`<m:FindItemResponse><m:ResponseMessages><m:FindItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode>
<m:RootFolder><t:Items><t:CalendarItem><t:ItemId Id="occ-2" ChangeKey="o2"/><t:Start>2000-01-01T00:00:00Z</t:Start><t:OriginalStart>` + func() string {
			from, _ := time.Parse(time.RFC3339, regexp.MustCompile(`StartDate="([^"]+)"`).FindStringSubmatch(body)[1])
			return from.Add(time.Minute).UTC().Format(time.RFC3339)
		}() + `</t:OriginalStart><t:UID>series@corp</t:UID></t:CalendarItem></t:Items></m:RootFolder></m:FindItemResponseMessage></m:ResponseMessages></m:FindItemResponse>`)
	case strings.Contains(body, "<m:UpdateItem"):
		id := regexp.MustCompile(`<t:ItemId Id="([^"]+)"( ChangeKey="([^"]+)")?`).FindStringSubmatch(body)
		if want := f.keys[id[1]]; want != "" && id[3] != want {
			reply(`<m:UpdateItemResponse><m:ResponseMessages><m:UpdateItemResponseMessage ResponseClass="Error"><m:MessageText>changed</m:MessageText><m:ResponseCode>ErrorIrresolvableConflict</m:ResponseCode></m:UpdateItemResponseMessage></m:ResponseMessages></m:UpdateItemResponse>`)
			return
		}
		f.updatedID = append(f.updatedID, id[1])
		reply(`<m:UpdateItemResponse><m:ResponseMessages><m:UpdateItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode>
<m:Items><t:CalendarItem><t:ItemId Id="` + id[1] + `" ChangeKey="k2"/></t:CalendarItem></m:Items></m:UpdateItemResponseMessage></m:ResponseMessages></m:UpdateItemResponse>`)
	case strings.Contains(body, "<m:DeleteItem"):
		f.deleted = append(f.deleted, regexp.MustCompile(`ItemId Id="([^"]+)"`).FindStringSubmatch(body)[1])
		reply(`<m:DeleteItemResponse><m:ResponseMessages><m:DeleteItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode></m:DeleteItemResponseMessage></m:ResponseMessages></m:DeleteItemResponse>`)
	default:
		f.t.Errorf("unexpected request %s", body)
		w.WriteHeader(500)
	}
}

func setup(t *testing.T) (*fakeExchange, *calsync.Engine) {
	t.Helper()
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	day := time.Now().Add(48 * time.Hour).Format("20060102")
	f := &fakeExchange{t: t, keys: map[string]string{"series": "s1", "invite": "i1", "own": "w1"}, items: map[string]string{
		// A weekly series written the Exchange way, with an exception.
		"series": "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTIMEZONE\r\nTZID:W. Europe Standard Time\r\nBEGIN:STANDARD\r\nDTSTART:16010101T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
			"BEGIN:VEVENT\r\nUID:series@corp\r\nSUMMARY:Team sync\r\nDTSTART;TZID=W. Europe Standard Time:" + day + "T100000\r\nDTEND;TZID=W. Europe Standard Time:" + day + "T103000\r\nRRULE:FREQ=WEEKLY;COUNT=4\r\nORGANIZER:mailto:boss@corp.com\r\nATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:me@corp.com\r\nEND:VEVENT\r\n" +
			"BEGIN:VEVENT\r\nUID:series@corp\r\nRECURRENCE-ID;TZID=W. Europe Standard Time:" + time.Now().Add(48*time.Hour+7*24*time.Hour).Format("20060102") + "T100000\r\nSUMMARY:Team sync (moved)\r\nDTSTART;TZID=W. Europe Standard Time:" + time.Now().Add(48*time.Hour+7*24*time.Hour).Format("20060102") + "T140000\r\nDTEND;TZID=W. Europe Standard Time:" + time.Now().Add(48*time.Hour+7*24*time.Hour).Format("20060102") + "T143000\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"invite": "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:invite@corp\r\nSUMMARY:Budget\r\nDTSTART:" + day + "T150000Z\r\nDTEND:" + day + "T160000Z\r\nORGANIZER:mailto:cfo@corp.com\r\nATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:me@corp.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"own":    "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:own@corp\r\nSUMMARY:Focus\r\nDTSTART:" + day + "T080000Z\r\nDTEND:" + day + "T090000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
	}}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	acct := store.CalendarAccount{ID: "x", Provider: "ews", Name: "me@corp.com", Email: "me@corp.com", Status: "ok",
		Config: map[string]string{"url": srv.URL + "/EWS/Exchange.asmx", "username": `CORP\me`, "auth": "basic"}}
	store.SaveCalendarAccount(acct, "pw")
	e := calsync.New()
	e.Register("ews", func(a store.CalendarAccount) (calsync.Provider, error) {
		p, err := New(a)
		if err != nil {
			return nil, err
		}
		p.(*Provider).client = srv.Client()
		return p, nil
	})
	return f, e
}

func TestExchangeSync(t *testing.T) {
	f, e := setup(t)
	if err := e.SyncAccount(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	cals, _ := store.ListCalendars()
	names := map[string]bool{}
	for _, c := range cals {
		names[c.Name] = true
	}
	if !names["Calendar"] || !names["Team"] {
		t.Errorf("calendars %v", names)
	}
	occ, _ := store.ListOccurrences(time.Now(), time.Now().Add(30*24*time.Hour), nil)
	got := map[string]int{}
	for _, o := range occ {
		got[o.Summary]++
	}
	if got["Team sync"] != 3 || got["Team sync (moved)"] != 1 || got["Budget"] != 1 || got["Focus"] != 1 {
		t.Errorf("occurrences %v", got)
	}
	// 10:00 in Berlin, read from a Windows zone name.
	for _, o := range occ {
		if o.Summary == "Team sync" {
			if h := o.Start.In(mustLoc("Europe/Berlin")).Hour(); h != 10 {
				t.Errorf("team sync at %d:00 Berlin", h)
			}
			break
		}
	}
	inv, _ := store.EventsByUID("", "invite@corp")
	if len(inv) != 1 || inv[0].RemoteID != "invite" || inv[0].ETag != "i1" || inv[0].MyStatus != "needs-action" {
		t.Errorf("invite %+v", inv)
	}
	_ = f
}

func mustLoc(n string) *time.Location { l, _ := time.LoadLocation(n); return l }

func TestExchangeWrites(t *testing.T) {
	f, e := setup(t)
	ctx := context.Background()
	e.SyncAccount(ctx, "x")
	root, _ := store.CalendarByRemote("x", "cal-root")

	// Create a weekly event with people: Exchange sends the invitations.
	start := time.Now().Add(72 * time.Hour).Truncate(time.Hour).UTC()
	r, err := e.Create(root.ID, calendar.Event{Summary: "Design & review", Start: start, End: start.Add(time.Hour),
		TZ: "America/New_York", Recurrence: []string{"RRULE:FREQ=WEEKLY;BYDAY=TU,TH;COUNT=6"}, Alarms: []int{15},
		Attendees: []calendar.Attendee{{Address: "ada@corp.com"}, {Address: "bob@corp.com", Role: "optional"}}}, calsync.OriginHuman)
	if err != nil {
		t.Fatal(err)
	}
	e.SyncAccount(ctx, "x")
	if len(f.created) != 1 {
		t.Fatalf("created %d", len(f.created))
	}
	c := f.created[0]
	for _, must := range []string{`SendMeetingInvitations="SendToAllAndSaveCopy"`, "<t:Subject>Design &amp; review</t:Subject>",
		"<t:DaysOfWeek>Tuesday Thursday</t:DaysOfWeek>", "<t:NumberOfOccurrences>6</t:NumberOfOccurrences>",
		`<t:StartTimeZone Id="Eastern Standard Time"/>`, "<t:OptionalAttendees>", "<t:ReminderMinutesBeforeStart>15<"} {
		if !strings.Contains(c, must) {
			t.Errorf("create lacks %s:\n%s", must, c)
		}
	}
	if ev, _ := store.GetEvent(r.EventID); ev.RemoteID != "made-1" {
		t.Errorf("remote id %q", ev.RemoteID)
	}

	// Accept the invitation.
	inv, _ := store.EventsByUID(root.ID, "invite@corp")
	if _, err := e.Respond(inv[0].ID, "accepted", "", calsync.OriginHuman); err != nil {
		t.Fatal(err)
	}
	e.SyncAccount(ctx, "x")
	if len(f.accepted) != 1 || f.accepted[0] != "invite" {
		t.Errorf("accepted %v", f.accepted)
	}

	// Change one occurrence of the series: found through the calendar view.
	ss, _ := store.EventsByUID(root.ID, "series@corp")
	var series store.StoredEvent
	for _, s := range ss {
		if s.RecurrenceID == nil {
			series = s
		}
	}
	occ := series.Start.Add(14 * 24 * time.Hour)
	if _, err := e.Update(series.ID, calsync.ScopeThis, occ, calendar.Event{Summary: "Team sync (short)", Start: occ, End: occ.Add(15 * time.Minute)}, calsync.OriginHuman); err != nil {
		t.Fatal(err)
	}
	e.SyncAccount(ctx, "x")
	if len(f.updatedID) == 0 || f.updatedID[len(f.updatedID)-1] != "occ-2" {
		t.Errorf("updated %v", f.updatedID)
	}

	// Someone changed "own" first: the ChangeKey no longer matches.
	f.keys["own"] = "changed-elsewhere"
	own, _ := store.EventsByUID(root.ID, "own@corp")
	u, _ := e.Update(own[0].ID, calsync.ScopeAll, time.Time{}, calendar.Event{Summary: "Focus (longer)", Start: own[0].Start, End: own[0].End.Add(time.Hour)}, calsync.OriginHuman)
	e.SyncAccount(ctx, "x")
	if ch, _ := store.GetCalendarChange(u.ChangeID); ch.State != "conflict" {
		t.Errorf("state %q", ch.State)
	}

	// Delete.
	if _, err := e.Delete(r.EventID, calsync.ScopeAll, time.Time{}, calsync.OriginHuman); err != nil {
		t.Fatal(err)
	}
	e.SyncAccount(ctx, "x")
	if len(f.deleted) != 1 || f.deleted[0] != "made-1" {
		t.Errorf("deleted %v", f.deleted)
	}
}

func TestExchangeRefusals(t *testing.T) {
	if _, err := newProvider(store.CalendarAccount{Config: map[string]string{"url": "http://mail.corp.com/EWS/Exchange.asmx"}}, "pw"); err == nil {
		t.Error("plain http accepted")
	}
	if _, err := recurrenceXML(calendar.Event{Start: time.Now(), Recurrence: []string{"RRULE:FREQ=HOURLY"}}); err != ErrUnsupportedRule {
		t.Errorf("hourly: %v", err)
	}
	got, err := recurrenceXML(calendar.Event{Start: time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC), Recurrence: []string{"RRULE:FREQ=MONTHLY;BYDAY=-1FR;UNTIL=20270101T000000Z"}})
	if err != nil || !strings.Contains(got, "<t:DayOfWeekIndex>Last</t:DayOfWeekIndex>") || !strings.Contains(got, "<t:EndDate>2027-01-01</t:EndDate>") {
		t.Errorf("last friday: %s %v", got, err)
	}
	if z := windowsZone("Europe/Paris"); z != "Romance Standard Time" {
		t.Errorf("paris = %q", z)
	}
}

func TestExchangeBadPassword(t *testing.T) {
	_, e := setup(t)
	store.SaveCalendarAccount(store.CalendarAccount{ID: "x", Provider: "ews", Name: "me", Status: "ok",
		Config: map[string]string{"url": "https://127.0.0.1:1/EWS/Exchange.asmx", "username": `CORP\me`, "auth": "basic"}}, "wrong")
	if err := e.SyncAccount(context.Background(), "x"); err == nil {
		t.Error("synced with a wrong password")
	}
}
