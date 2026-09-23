package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/inboxql/internal/store"
)

// # The collision this guards
//
// The listing row embeds *store.Annotator and adds its own fields. The
// annotator now carries Scope — the query narrowing what it runs over — and
// the row used to have a Scope of its own meaning something else entirely:
// whether its model profile is local or remote.
//
// Go resolves that silently in favour of the outer field, so the annotator's
// scope simply vanished from every response, and the two meanings were one
// word apart in the frontend. The outer one is now "reach".
func TestAnnotatorListingKeepsScopeAndReachApart(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer store.CloseDB()

	if err := store.SaveAnnotator(&store.Annotator{
		Name: "gated", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"amount":"x"}`,
		Scope: "label:money", Trigger: store.TriggerAfterSync,
	}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	listAnnotators(rr, httptest.NewRequest(http.MethodGet, "/api/annotators", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d annotators, want 1", len(got))
	}

	if got[0]["scope"] != "label:money" {
		t.Errorf("scope = %v, want the annotator's query — it is being shadowed", got[0]["scope"])
	}
	if got[0]["trigger"] != store.TriggerAfterSync {
		t.Errorf("trigger = %v, want %q", got[0]["trigger"], store.TriggerAfterSync)
	}
	// A local engine has no gateway, so there is no reach to report.
	if _, ok := got[0]["reach"]; ok {
		t.Errorf("reach = %v on a gliner annotator, which reaches nothing", got[0]["reach"])
	}
}

// Coverage is measured against what the annotator actually covers. An
// extractor gated by a label has finished when it has read that label's
// messages, and "9 / 188" for a complete job reads as 5% done.
func TestAnnotatorCoverageIsAgainstItsOwnScope(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer store.CloseDB()

	if err := store.SaveAnnotator(&store.Annotator{
		Name: "gate", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAnnotator(&store.Annotator{
		Name: "gated", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"amount":"x"}`,
		Scope: "label:gate",
	}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	listAnnotators(rr, httptest.NewRequest(http.MethodGet, "/api/annotators", nil))

	var got []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	for _, a := range got {
		p, _ := a["progress"].(map[string]any)
		if p == nil {
			t.Fatalf("%v has no progress", a["name"])
		}
		total, _ := p["total"].(float64)
		// Nothing has run, so the gate matches nothing and the gated
		// annotator's scope is empty. The point is that it is measured
		// against the scope at all rather than against every message.
		if a["name"] == "gated" && total != 0 {
			t.Errorf("gated total = %v, want 0 — its gate has matched nothing yet", total)
		}
	}
}

// A scope naming a label that no longer exists must not take the whole panel
// down with it. Before this, one deleted gate label made GET /api/annotators
// return 500 — so the list that would let you edit or delete the broken one
// could not load at all.
func TestABrokenScopeDoesNotFailTheListing(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer store.CloseDB()

	if err := store.SaveAnnotator(&store.Annotator{
		Name: "orphan", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"amount":"x"}`,
		Scope: "label:deletedlongago",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAnnotator(&store.Annotator{
		Name: "healthy", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread",
	}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	listAnnotators(rr, httptest.NewRequest(http.MethodGet, "/api/annotators", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("one broken scope failed the listing: %d %s", rr.Code, rr.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d annotators, want both", len(got))
	}

	for _, a := range got {
		if a["name"] == "orphan" && a["scopeBroken"] != true {
			t.Errorf("the broken scope was not flagged: %v", a)
		}
		if a["name"] == "healthy" && a["scopeBroken"] == true {
			t.Errorf("a working annotator was flagged broken")
		}
	}
}
