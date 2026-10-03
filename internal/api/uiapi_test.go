package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/uisession"
)

func freshSessions(t *testing.T) {
	t.Helper()
	old := sessions
	sessions = uisession.New()
	t.Cleanup(func() { sessions = old })
}

func post(t *testing.T, path, body string, h http.HandlerFunc, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

// Registering hands back a name a person can type, and the timings the client
// needs to keep itself alive.
func TestRegisterReturnsATypeableName(t *testing.T) {
	freshSessions(t)

	rr := post(t, "/api/ui/register", `{"title":"Chrome"}`, handleUIRegister, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Name        string `json:"name"`
		HeartbeatMs int64  `json:"heartbeatMs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name == "" || len(body.Name) > 4 {
		t.Errorf("name %q is not something to type", body.Name)
	}
	if body.HeartbeatMs <= 0 {
		t.Error("the client was not told how often to beat")
	}
}

// The heartbeat carries what is on screen, because "is it alive" and "what is
// it showing" are the same question asked of the same window — and splitting
// them would let the listing go stale while the window stayed alive.
func TestStateIsTheHeartbeat(t *testing.T) {
	freshSessions(t)
	s := sessions.Register("Chrome")

	rr := post(t, "/api/ui/"+s.Name+"/state",
		`{"query":"folder:inbox | timeline","tabs":["desk"],"active":"desk"}`,
		handleUIState, map[string]string{"name": s.Name})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}

	got := sessions.Get(s.Name)
	if got.Showing.Query != "folder:inbox | timeline" {
		t.Errorf("query = %q", got.Showing.Query)
	}
}

// A window whose registration has gone — expired while the machine slept, or
// the server restarted — is told so, rather than left heartbeating into
// nothing forever.
func TestAnUnknownWindowIsToldToRegisterAgain(t *testing.T) {
	freshSessions(t)

	rr := post(t, "/api/ui/nope/state", `{}`, handleUIState, map[string]string{"name": "nope"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 so the client registers again", rr.Code)
	}
}

// The listing is the half that answers "what is on my other screen", for a
// person and for an agent.
func TestListSaysWhatEachWindowIsShowing(t *testing.T) {
	freshSessions(t)
	a := sessions.Register("one")
	b := sessions.Register("two")
	sessions.Touch(a.Name, uisession.Showing{Query: "from:stripe"})
	sessions.Touch(b.Name, uisession.Showing{Query: "in:logs level>warn"})

	rr := httptest.NewRecorder()
	handleUIList(rr, httptest.NewRequest(http.MethodGet, "/api/ui", nil))

	var body struct {
		Windows []*uisession.Session `json:"windows"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(body.Windows))
	}
	if body.Windows[0].Showing.Query != "from:stripe" {
		t.Errorf("first window shows %q", body.Windows[0].Showing.Query)
	}
}

// A verb this build does not understand is refused rather than delivered, so a
// typo fails here instead of being ignored in a browser where nobody sees it.
func TestAnUnknownVerbIsRefused(t *testing.T) {
	freshSessions(t)
	s := sessions.Register("one")
	sessions.Connect(s.Name)

	rr := post(t, "/api/ui/"+s.Name+"/command", `{"verb":"destroy","arg":"x"}`,
		handleUICommand, map[string]string{"name": s.Name})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rr.Code)
	}
}

// The one that matters: a command to a window that is not there must not
// report success.
func TestCommandingAMissingWindowIs404(t *testing.T) {
	freshSessions(t)

	rr := post(t, "/api/ui/7/command", `{"verb":"query","arg":"x"}`,
		handleUICommand, map[string]string{"name": "7"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 — a command that appears to work and does nothing is the "+
			"failure this whole surface has to avoid", rr.Code)
	}
}

func TestCommandReachesTheWindow(t *testing.T) {
	freshSessions(t)
	s := sessions.Register("one")
	ch, _, _ := sessions.Connect(s.Name)

	rr := post(t, "/api/ui/"+s.Name+"/command",
		`{"verb":"query","arg":"from:stripe","note":"the invoices you asked about"}`,
		handleUICommand, map[string]string{"name": s.Name})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}

	got := <-ch
	if got.Arg != "from:stripe" {
		t.Errorf("arg = %q", got.Arg)
	}
	// The note travels with the command rather than separately: a screen that
	// reorganises itself unannounced is alarming, and the explanation has to
	// arrive with the change.
	if got.Note != "the invoices you asked about" {
		t.Errorf("note = %q", got.Note)
	}
}
