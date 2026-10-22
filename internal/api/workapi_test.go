package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// The Work loop page's surface: metrics, a disagreement settled, a target set.
func TestWorkLoopPage(t *testing.T) {
	setupTestDB(t)
	if err := store.SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := store.SaveMessage(&message.Message{ID: "r1", AccountID: "acct", MessageID: "<r1@x>",
		From: "shop@example.com", To: []string{"me@example.com"}, Subject: "Your receipt", Body: "TOTAL 12.00",
		Date: now, InternalDate: now, ContentHash: "r1", Mailbox: "INBOX"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := elements.Install(nil, false); err != nil {
		t.Fatal(err)
	}
	no := false
	if err := store.SaveAudit(&store.WorkAudit{Thread: "r1@x", MessageID: "r1", Target: "r1", Element: "receipts",
		Stratum: store.StratumRandom, Original: `{"none":"none"}`, OriginalModel: "fast",
		Checker: `{"records":[{"merchant":"Shop","total":12,"quote":"TOTAL 12.00"}]}`, CheckerModel: "careful",
		Agree: &no}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerWorkRoutes(mux)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	rr := call(http.MethodGet, "/api/work/metrics?since=7d", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("metrics: %d %s", rr.Code, rr.Body.String())
	}
	var m struct {
		Checks   struct{ Open int } `json:"checks"`
		Elements []struct {
			Element string `json:"element"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Checks.Open != 1 || len(m.Elements) != len(elements.Catalog) {
		t.Errorf("metrics: open=%d elements=%d", m.Checks.Open, len(m.Elements))
	}
	if rr := call(http.MethodGet, "/api/work/metrics?since=yesterday", ""); rr.Code != http.StatusBadRequest {
		t.Errorf("a bad period was accepted: %d", rr.Code)
	}

	rr = call(http.MethodGet, "/api/work/disagreements", "")
	var d struct {
		Disagreements []struct {
			ID           string `json:"id"`
			Subject      string `json:"subject"`
			OriginalSays string `json:"originalSays"`
			CheckerSays  string `json:"checkerSays"`
			Question     string `json:"question"`
		} `json:"disagreements"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil || len(d.Disagreements) != 1 {
		t.Fatalf("disagreements: %v %s", err, rr.Body.String())
	}
	got := d.Disagreements[0]
	if got.Subject != "Your receipt" || got.OriginalSays != "nothing here" || !strings.Contains(got.CheckerSays, "Shop") || got.Question == "" {
		t.Errorf("disagreement as shown: %+v", got)
	}

	if rr := call(http.MethodPost, "/api/work/disagreements/resolve", `{"id":"`+got.ID+`","choice":"checker"}`); rr.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", rr.Code, rr.Body.String())
	}
	if rr := call(http.MethodPost, "/api/work/disagreements/resolve", `{"id":"`+got.ID+`","choice":"checker"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("settling twice: %d", rr.Code)
	}
	if rr := call(http.MethodPost, "/api/work/disagreements/resolve", `{"id":"nope","choice":"checker"}`); rr.Code == http.StatusOK {
		t.Error("an unknown check was settled")
	}

	rr = call(http.MethodPut, "/api/work/targets", `{"element":"receipts","target":0.99,"cost":"high"}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"target":0.99`) {
		t.Errorf("set target: %d %s", rr.Code, rr.Body.String())
	}
	if rr := call(http.MethodPut, "/api/work/targets", `{"element":"receipts","target":2}`); rr.Code != http.StatusBadRequest {
		t.Errorf("an impossible target was accepted: %d", rr.Code)
	}

	if rr := call(http.MethodPost, "/api/work/flags", `{"thread":"r1@x","reason":"look again"}`); rr.Code != http.StatusOK {
		t.Fatalf("flag: %d", rr.Code)
	}
	if rr := call(http.MethodGet, "/api/work/flags", ""); !strings.Contains(rr.Body.String(), "look again") {
		t.Errorf("flags: %s", rr.Body.String())
	}
	if rr := call(http.MethodGet, "/api/work/backlog", ""); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"receipts"`) {
		t.Errorf("backlog: %d %s", rr.Code, rr.Body.String())
	}
}
