package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
)

func calendarTest(t *testing.T) *http.ServeMux {
	t.Helper()
	setupTestDB(t)
	SetCalendarEngine(calsync.New())
	mux := http.NewServeMux()
	registerCalendarRoutes(mux)
	return mux
}

func TestCalendarCreateViewEditDelete(t *testing.T) {
	mux := calendarTest(t)
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour).UTC()
	rec, out := call(t, mux, "POST", "/api/calendar/events", map[string]any{"event": map[string]any{
		"summary": "Planning", "start": start, "end": start.Add(time.Hour), "tz": "Europe/London",
		"attendees": []map[string]any{{"address": "Ada@Example.com"}}}})
	if rec.Code != 201 || out["state"] != "applied" {
		t.Fatalf("create %d %v", rec.Code, out)
	}
	id := out["eventId"].(string)
	q := "/api/calendar/occurrences?from=" + start.Add(-time.Hour).Format(time.RFC3339) + "&to=" + start.Add(2*time.Hour).Format(time.RFC3339)
	_, out = call(t, mux, "GET", q, nil)
	if occ := out["occurrences"].([]any); len(occ) != 1 {
		t.Fatalf("occurrences %v", out)
	}
	_, out = call(t, mux, "GET", "/api/calendar/events/"+id, nil)
	ev := out["event"].(map[string]any)
	if ev["summary"] != "Planning" || out["writable"] != true || len(ev["attendees"].([]any)) != 1 {
		t.Errorf("event %v", out)
	}
	rec, _ = call(t, mux, "PUT", "/api/calendar/events/"+id, map[string]any{"event": map[string]any{
		"summary": "Planning (longer)", "start": start, "end": start.Add(2 * time.Hour)}})
	if rec.Code != 200 {
		t.Errorf("update %d %s", rec.Code, rec.Body)
	}
	if e, _ := store.GetEvent(id); e.Summary != "Planning (longer)" {
		t.Errorf("after update %q", e.Summary)
	}
	if rec, _ := call(t, mux, "POST", "/api/calendar/events", map[string]any{"event": map[string]any{"summary": "", "start": start, "end": start}}); rec.Code != 400 {
		t.Errorf("untitled event = %d", rec.Code)
	}
	if rec, _ := call(t, mux, "POST", "/api/calendar/events", map[string]any{"calendarId": store.MailCalendarID,
		"event": map[string]any{"summary": "x", "start": start, "end": start}}); rec.Code != 403 {
		t.Errorf("into invitations = %d", rec.Code)
	}
	if rec, _ := call(t, mux, "DELETE", "/api/calendar/events/"+id, nil); rec.Code != 200 {
		t.Errorf("delete %d", rec.Code)
	}
	_, out = call(t, mux, "GET", q, nil)
	if occ := out["occurrences"].([]any); len(occ) != 0 {
		t.Errorf("after delete %v", out)
	}
}

// A proposal is approved from the page, not by a script.
func TestApproveNeedsThePage(t *testing.T) {
	mux := calendarTest(t)
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour).UTC()
	r, err := engine().Create(store.LocalCalendarID, EventInput{Summary: "Agent idea", Start: start, End: start.Add(time.Hour)}.model(), calsync.OriginAgent)
	if err != nil {
		t.Fatal(err)
	}
	if rec, _ := call(t, mux, "POST", "/api/calendar/changes/"+r.ChangeID+"/approve", nil); rec.Code != http.StatusForbidden {
		t.Errorf("script approve = %d, want 403", rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/calendar/changes/"+r.ChangeID+"/approve", strings.NewReader(""))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("page approve = %d %s", rec.Code, rec.Body)
	}
	if e, _ := store.GetEvent(r.EventID); e.Proposed {
		t.Error("still proposed after approval")
	}
}

func TestImportExportRoundTrip(t *testing.T) {
	mux := calendarTest(t)
	start := time.Now().Add(48 * time.Hour).UTC().Format("20060102T150000Z")
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:imp1\r\nSUMMARY:Imported\r\nDTSTART:" + start + "\r\nDURATION:PT1H\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	req := httptest.NewRequest("POST", "/api/calendar/import", strings.NewReader(ics))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"imported":1`) {
		t.Fatalf("import %d %s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest("GET", "/api/calendar/export?calendar="+store.LocalCalendarID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "SUMMARY:Imported") || !strings.Contains(rec.Body.String(), "RRULE:FREQ=DAILY;COUNT=3") {
		t.Errorf("export %d %s", rec.Code, rec.Body)
	}
}

// Lists are lists: an invitation with nothing clashing says [], not null,
// which a page reading .length on it would fall over.
func TestInvitationClashesIsAList(t *testing.T) {
	mux := calendarTest(t)
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour).UTC()
	id, _ := store.UpsertEvent(store.MailCalendarID, EventInput{Summary: "x", Start: start, End: start.Add(time.Hour)}.model(),
		store.EventMeta{SourceMessageID: "m1", Origin: "mail"})
	_ = id
	rec, _ := call(t, mux, "GET", "/api/calendar/invitation?message=m1", nil)
	if strings.Contains(rec.Body.String(), "null") {
		t.Errorf("a list came back null: %s", rec.Body)
	}
	_, out := call(t, mux, "GET", "/api/calendar/events/"+id, nil)
	if out["changes"] == nil {
		t.Errorf("changes is null: %v", out)
	}
}
