package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// Each widget's groups carry the term that is exactly that group, so a click
// opens the mail the bar counted; the calendar counts the year it draws.
func TestAnalyticsGroupsCarryTheirTerm(t *testing.T) {
	setupTestDB(t)
	if err := store.SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	save := func(id, from, subject string, date time.Time) {
		t.Helper()
		if err := store.SaveMessage(&message.Message{ID: id, AccountID: "acct", MessageID: "<" + id + "@x>",
			From: from, To: []string{"me@example.com"}, Subject: subject, Body: "b", Date: date,
			InternalDate: date, ContentHash: id, Mailbox: "INBOX"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.Local)
	save("m1", "alice@acme.com", "Invoice for May", now)
	save("m2", "notalice@acme.com", "Invoice again", now)
	save("m3", "alice@acme.com", "Lunch plans", now.AddDate(-1, 0, 0))
	save("m4", "ci@acme.com", "[acme/web] Build failed", now)
	if err := store.SetMessageTopics("m3", []string{"Food"}, "test", nil); err != nil {
		t.Fatal(err)
	}

	get := func(params string) []analyticsGroup {
		t.Helper()
		rr := httptest.NewRecorder()
		handleAnalytics(rr, httptest.NewRequest(http.MethodGet, "/api/analytics?"+params, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", params, rr.Code, rr.Body.String())
		}
		var out []analyticsGroup
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	q := url.QueryEscape("folder:inbox")

	terms := map[string]string{}
	for _, g := range get("type=senders&q=" + q) {
		terms[g.Label] = g.Term
	}
	if terms["alice@acme.com"] != "from:=alice@acme.com" {
		t.Errorf("sender term = %q, want the exact from:=alice@acme.com", terms["alice@acme.com"])
	}

	kinds := map[string]analyticsGroup{}
	for _, g := range get("type=topics&q=" + q) {
		kinds[g.Label] = g
	}
	if g := kinds["Food"]; g.Kind != "topic" || g.Term != "topic:=Food" {
		t.Errorf("an extracted topic = %+v, want kind topic and topic:=Food", g)
	}
	if g := kinds["invoice"]; g.Kind != "word" || g.Term != "subject:invoice" {
		t.Errorf("a first word = %+v, want kind word and subject:invoice", g)
	}
	// A first word is trimmed of its punctuation, and its click finds it.
	g, ok := kinds["acme/web"]
	if !ok {
		t.Fatalf("topics = %+v, want the bracketed first word as acme/web", kinds)
	}
	if n, err := store.CountQuery("folder:inbox " + g.Term); err != nil || n != int64(g.Value) {
		t.Errorf("%s matched %d (%v), want the %v the bar counted", g.Term, n, err, g.Value)
	}

	total := 0.0
	for _, g := range get("type=volume&year=2026&q=" + q) {
		total += g.Value
	}
	if total != 3 {
		t.Errorf("2026 volume = %v, want the three messages of 2026 only", total)
	}
	rr := httptest.NewRecorder()
	handleAnalytics(rr, httptest.NewRequest(http.MethodGet, "/api/analytics?type=volume&year=26", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("a bad year = %d, want 400", rr.Code)
	}

	for _, g := range get("type=sentby&q=" + q) {
		if g.Term != "sentby:"+g.Label {
			t.Errorf("sentby group %+v has term %q", g, g.Term)
		}
	}
	for _, g := range get("type=importance&q=" + q) {
		if g.Label == "(not judged)" && g.Term != "" {
			t.Errorf("a group no term can name has term %q", g.Term)
		}
	}
}
