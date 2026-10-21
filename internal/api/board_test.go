package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/store"
)

// boardCall sends one request through the board's routes and the ticket
// handler, as the page does.
func boardCall(t *testing.T, method, path, body string, out any) int {
	t.Helper()
	mux := http.NewServeMux()
	registerBoardRoutes(mux)
	registerQueryRoutes(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if out != nil && rr.Code < 400 {
		if err := json.Unmarshal(rr.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, rr.Body.String())
		}
	}
	if rr.Code >= 400 {
		t.Logf("%s %s: %d %s", method, path, rr.Code, rr.Body.String())
	}
	return rr.Code
}

func TestTheBoardOverHTTP(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()

	// A list of your own, after To do.
	var list store.TicketList
	if code := boardCall(t, "POST", "/api/board/lists", `{"title":"Waiting","after":"todo"}`, &list); code != 200 {
		t.Fatalf("create list: %d", code)
	}

	// Quick-add on a board: the card arrives with the board's label.
	var a, b store.TicketDetail
	if code := boardCall(t, "POST", "/api/tickets", `{"title":"Call the plumber","status":"todo","labels":["home"]}`, &a); code != 201 {
		t.Fatalf("create card: %d", code)
	}
	if len(a.Labels) != 1 || a.Labels[0] != "home" {
		t.Errorf("labels = %v", a.Labels)
	}
	boardCall(t, "POST", "/api/tickets", `{"title":"Fix the gate","status":"todo"}`, &b)

	// Edit in place: a partial edit leaves the rest alone, and null clears.
	var edited store.TicketDetail
	if code := boardCall(t, "PATCH", "/api/tickets?id="+a.ID,
		`{"body":"Ask about the **boiler**","dueAt":"2026-11-02","startAt":"2026-10-30"}`, &edited); code != 200 {
		t.Fatalf("patch: %d", code)
	}
	if edited.Title != "Call the plumber" || edited.DueAt == nil || edited.StartAt == nil ||
		!strings.Contains(edited.BodyHTML, "<strong>boiler</strong>") {
		t.Errorf("after patch: %+v", edited)
	}
	var cleared store.TicketDetail
	boardCall(t, "PATCH", "/api/tickets?id="+a.ID, `{"dueAt":null}`, &cleared)
	if cleared.DueAt != nil || cleared.StartAt == nil {
		t.Errorf("null cleared the wrong date: due %v start %v", cleared.DueAt, cleared.StartAt)
	}

	// Move into the new list, above the gate after it follows.
	if code := boardCall(t, "POST", "/api/board/move", `{"id":"`+b.ID+`","list":"waiting"}`, nil); code != 200 {
		t.Fatalf("move: %d", code)
	}
	if code := boardCall(t, "POST", "/api/board/move", `{"id":"`+a.ID+`","list":"waiting","after":"^"}`, nil); code != 200 {
		t.Fatalf("move to top: %d", code)
	}
	var cols []store.BoardColumn
	boardCall(t, "GET", "/api/tickets/board", "", &cols)
	var waiting *store.BoardColumn
	for i := range cols {
		if cols[i].Status == "waiting" {
			waiting = &cols[i]
		}
	}
	if waiting == nil || waiting.Title != "Waiting" || len(waiting.Tickets) != 2 ||
		waiting.Tickets[0].Title != "Call the plumber" {
		t.Fatalf("waiting column = %+v", waiting)
	}

	// Checklist, comment, and the counts on the front of the card.
	var d store.TicketDetail
	boardCall(t, "POST", "/api/board/checklist", `{"ticketId":"`+a.ID+`","text":"Find the number"}`, &d)
	if len(d.Checklist) != 1 {
		t.Fatalf("checklist = %+v", d.Checklist)
	}
	if code := boardCall(t, "PATCH", "/api/board/checklist", `{"id":"`+d.Checklist[0].ID+`","done":true}`, nil); code != 200 {
		t.Fatalf("tick: %d", code)
	}
	boardCall(t, "POST", "/api/board/comments", `{"ticketId":"`+a.ID+`","body":"Rang, no answer"}`, &d)
	if len(d.Notes) != 1 || d.Notes[0].Actor != "human" {
		t.Errorf("comments = %+v", d.Notes)
	}

	// A board named for a label, and the board endpoint reading it.
	if code := boardCall(t, "POST", "/api/board/boards", `{"title":"Home","filter":"label:home"}`, nil); code != 200 {
		t.Fatalf("create board: %d", code)
	}
	boardCall(t, "GET", "/api/tickets/board?board=home", "", &cols)
	total := 0
	for _, c := range cols {
		total += len(c.Tickets)
		for _, tk := range c.Tickets {
			if tk.ChecklistDone != 1 || tk.ChecklistTotal != 1 || tk.Comments != 1 {
				t.Errorf("front of card = %d/%d, %d", tk.ChecklistDone, tk.ChecklistTotal, tk.Comments)
			}
		}
	}
	if total != 1 {
		t.Errorf("the Home board shows %d cards, want 1", total)
	}

	// Archive takes it off the board.
	boardCall(t, "POST", "/api/board/archive", `{"id":"`+a.ID+`","archived":true}`, nil)
	boardCall(t, "GET", "/api/tickets/board?board=home", "", &cols)
	for _, c := range cols {
		if len(c.Tickets) != 0 {
			t.Errorf("an archived card is still on the board: %v", c.Tickets)
		}
	}

	// A list with cards is not deleted without somewhere to put them.
	if code := boardCall(t, "DELETE", "/api/board/lists?id=waiting", "", nil); code != 400 {
		t.Errorf("deleting a list with cards: %d, want 400", code)
	}
	if code := boardCall(t, "DELETE", "/api/board/lists?id=waiting&moveTo=todo", "", nil); code != 200 {
		t.Errorf("deleting with a destination: %d", code)
	}

	// Rules: a bad one is refused where it is written.
	if code := boardCall(t, "POST", "/api/board/rules", `{"trigger":"enter-list","list":"done","actions":[{"do":"explode"}]}`, nil); code != 400 {
		t.Errorf("a bad rule: %d, want 400", code)
	}
	var rule store.TicketRule
	if code := boardCall(t, "POST", "/api/board/rules", `{"trigger":"enter-list","list":"done","actions":[{"do":"check-all"}]}`, &rule); code != 200 || rule.ID == "" {
		t.Errorf("a good rule: %d %+v", code, rule)
	}
}
