package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/store"
)

// A switched-off annotator is not offered in the message viewer, because
// pressing the control would refuse. What it already wrote is still drawn —
// those spans come back under `fields`, from a different handler, and are not
// affected by this.
func TestOffersSkipASwitchedOffAnnotator(t *testing.T) {
	list := []*store.Annotator{
		{ID: "a1", Name: "on", Kind: store.KindLabel, Engine: store.EngineRule, Enabled: true},
		{ID: "a2", Name: "off", Kind: store.KindLabel, Engine: store.EngineRule, Enabled: false},
	}

	got := offersFor(list, nil)
	if len(got) != 1 || got[0].Name != "on" {
		names := []string{}
		for _, o := range got {
			names = append(names, o.Name)
		}
		t.Fatalf("offered %v, want just [on]", names)
	}
}

// PUT with no `enabled` field must not read as "switch it off". A bool's zero
// value is false, so the one mistake this endpoint could make is treating an
// absent field as a decision.
func TestSetAnnotatorEnabledRefusesAnAbsentField(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()

	if err := store.SaveAnnotator(&store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/annotators?name=money",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleAnnotators(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rr.Code, rr.Body.String())
	}
	a, err := store.GetAnnotator("money")
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if !a.Enabled {
		t.Error("an empty body switched it off")
	}
}

func TestSetAnnotatorEnabledRoundTrip(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()

	if err := store.SaveAnnotator(&store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread",
	}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []bool{false, true} {
		body := `{"enabled":false}`
		if want {
			body = `{"enabled":true}`
		}
		req := httptest.NewRequest(http.MethodPut, "/api/annotators?name=money",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handleAnnotators(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}

		var got struct {
			Annotator struct {
				Enabled bool `json:"enabled"`
			} `json:"annotator"`
			Held *store.AnnotationVolume `json:"held"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Annotator.Enabled != want {
			t.Errorf("response says enabled=%v, want %v", got.Annotator.Enabled, want)
		}
		// The panel says what switching off kept, so the response has to carry
		// the number rather than leaving it to be guessed from coverage.
		if got.Held == nil {
			t.Error("the response does not say what it is holding")
		}
		a, err := store.GetAnnotator("money")
		if err != nil || a == nil {
			t.Fatal(err)
		}
		if a.Enabled != want {
			t.Errorf("stored enabled=%v, want %v", a.Enabled, want)
		}
	}
}

// # The requirement this guards
//
// The brief was a chooser that avoids creating work in either direction. A
// tick-box over install/delete fails that twice: unticking destroys every
// result and every human correction, and re-ticking buys back only the first,
// at about twenty seconds a message.
//
// So the tick means participation. Ticking something absent creates it inert;
// unticking something present is one UPDATE and keeps everything.
func TestTickingAStarterCreatesItAndUntickingKeepsIt(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()

	const name = "purchased"

	set := func(on bool) map[string]any {
		t.Helper()
		body := `{"enabled":false}`
		if on {
			body = `{"enabled":true}`
		}
		req := httptest.NewRequest(http.MethodPut, "/api/starters/"+name,
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("name", name)
		rr := httptest.NewRecorder()
		handleSetStarter(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Absent → ticked creates it, and creating is not running.
	out := set(true)
	if out["created"] != true {
		t.Errorf("ticking an absent starter did not create it: %v", out)
	}
	a, err := store.GetAnnotator(name)
	if err != nil || a == nil {
		t.Fatalf("it was not created: %v", err)
	}
	if !a.Enabled {
		t.Error("it arrived switched off")
	}
	p, err := store.Progress(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Evaluated != 0 {
		t.Errorf("creating it ran it: %d evaluated", p.Evaluated)
	}

	// Ticked → unticked keeps the row. This is the whole design: the
	// annotator, its version and everything it ever said survive.
	set(false)
	a, err = store.GetAnnotator(name)
	if err != nil || a == nil {
		t.Fatalf("unticking deleted it: %v", err)
	}
	if a.Enabled {
		t.Error("it is still on")
	}

	// And back, without a second create.
	out = set(true)
	if out["created"] == true {
		t.Errorf("re-ticking created a second one: %v", out)
	}
	a, err = store.GetAnnotator(name)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if !a.Enabled {
		t.Error("it did not come back on")
	}
}

// Unticking something that was never installed is already true, and must not
// create it in order to switch it off.
func TestUntickingAnAbsentStarterCreatesNothing(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()

	req := httptest.NewRequest(http.MethodPut, "/api/starters/purchased",
		strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "purchased")
	rr := httptest.NewRecorder()
	handleSetStarter(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	a, err := store.GetAnnotator("purchased")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil {
		t.Error("switching off an absent starter installed it")
	}
}

// The listing has to distinguish three states, because the chooser draws
// absent and off the same way and must not confuse them for the rest of what
// it says.
func TestStarterListingReportsOffSeparatelyFromAbsent(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()

	if _, _, err := installOneStarter(t, "purchased"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAnnotatorEnabled("purchased", false); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handleListStarters(rr, httptest.NewRequest(http.MethodGet, "/api/starters", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}

	var got []StarterInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	var seen bool
	for _, s := range got {
		if s.Name == "purchased" {
			seen = true
			if !s.Installed {
				t.Error("a switched-off starter reports as not installed")
			}
			if s.Enabled {
				t.Error("it reports as on")
			}
		}
		if s.Name != "purchased" && s.Installed {
			t.Errorf("%s reports installed and nothing installed it", s.Name)
		}
	}
	if !seen {
		t.Fatal("purchased is not in the pack listing")
	}
}

// A gate switched off freezes what it gates, for new mail only. Easy to do by
// accident two rows apart in the same list, and invisible without being told.
func TestStarterListingNamesASwitchedOffGate(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()

	if _, _, err := installOneStarter(t, "purchased"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := installOneStarter(t, "receipts"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAnnotatorEnabled("purchased", false); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handleListStarters(rr, httptest.NewRequest(http.MethodGet, "/api/starters", nil))

	var got []StarterInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.Name == "receipts" && s.GateOff != "purchased" {
			t.Errorf("receipts does not report its gate as off: %+v", s)
		}
		if s.Name == "purchased" && s.GateOff != "" {
			t.Errorf("purchased reports a gate of its own: %q", s.GateOff)
		}
	}
}

// installOneStarter is the install path the chooser takes, in test form.
func installOneStarter(t *testing.T, name string) ([]string, []string, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/starters",
		strings.NewReader(`{"names":["`+name+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleInstallStarters(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("installing %s: %d %s", name, rr.Code, rr.Body.String())
	}
	var out struct {
		Created []string `json:"created"`
		Skipped []string `json:"skipped"`
	}
	err := json.Unmarshal(rr.Body.Bytes(), &out)
	return out.Created, out.Skipped, err
}
