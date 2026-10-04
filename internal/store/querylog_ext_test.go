package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/logging"
	"github.com/user/inboxql/internal/store"
)

// # Why these live outside the package
//
// `logging` imports `store`, so a test inside `store` cannot import the logger
// to drive it. Outside, it can — and the test is better for it: it runs real
// queries through `RunQuery` and `CountQuery` and reads what came out, rather
// than calling an unexported helper and trusting that the real path calls it
// the same way.

func openLoggedFixture(t *testing.T, cfg store.LogSettings) {
	t.Helper()
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	if err := store.SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@x.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLogSettings(cfg); err != nil {
		t.Fatal(err)
	}
	logging.Start()
	logging.SetLevel(store.LevelDebug)
	t.Cleanup(logging.Stop)
}

func recorded(t *testing.T) []*store.LoggedError {
	t.Helper()
	logging.Stop()
	got, err := store.ListErrors(store.ErrorQuery{Category: "query"})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func settings(slow int, queryText bool, on bool) store.LogSettings {
	return store.LogSettings{
		SlowMs: slow, Retain: 10_000, QueryText: queryText,
		Categories: map[string]bool{"query": on},
	}
}

// # The point of the feature
//
// A duration in the message text cannot be asked about. `in:logs duration>1000`
// is the question a log exists to answer, and it needs a column.
func TestAQueryIsTimedIntoItsOwnColumn(t *testing.T) {
	openLoggedFixture(t, settings(500, true, true))

	if _, err := store.RunQuery("folder:inbox", 10, 0); err != nil {
		t.Fatal(err)
	}

	lines := recorded(t)
	if len(lines) == 0 {
		t.Fatal("running a query recorded nothing")
	}
	if lines[0].Duration == nil {
		t.Fatal("the line carries no duration; it is prose, not a number")
	}
	if lines[0].Reference != "folder:inbox" {
		t.Errorf("the query recorded was %q", lines[0].Reference)
	}
}

// The second path a query can take. An untimed one is where a slow query hides,
// and this is the busier of the two — reach counts and progress bars all come
// through it.
func TestACountIsTimedToo(t *testing.T) {
	openLoggedFixture(t, settings(500, true, true))

	if _, err := store.CountQuery("folder:inbox"); err != nil {
		t.Fatal(err)
	}
	if len(recorded(t)) == 0 {
		t.Error("CountQuery went untimed")
	}
}

// Every query at debug, anything slow at warn — so the default level surfaces
// exactly "this took 2.2 seconds" and nothing else.
//
// Asserted against the duration that was actually recorded rather than against
// a sleep, because a test that waits for a threshold is a test that fails on a
// fast machine.
func TestSlowQueriesBecomeWarnings(t *testing.T) {
	openLoggedFixture(t, settings(1, true, true))
	if _, err := store.RunQuery("folder:inbox", 10, 0); err != nil {
		t.Fatal(err)
	}

	for _, l := range recorded(t) {
		if l.Duration == nil {
			t.Fatal("no duration to judge against")
		}
		slow := *l.Duration >= 1
		switch {
		case slow && l.Level != store.LevelWarn:
			t.Errorf("%d ms against a 1 ms threshold is %q, want warn", *l.Duration, l.Level)
		case slow && !strings.Contains(l.Message+l.Context, "SELECT"):
			// The statement, so it can be pasted into `iql sql --explain`.
			// That is the reason the row is worth having.
			t.Errorf("a slow query carries no SQL: %+v", l)
		case !slow && l.Level != store.LevelDebug:
			t.Errorf("%d ms under the threshold is %q, want debug", *l.Duration, l.Level)
		}
	}
}

// Under the threshold, a query is a debug line and carries no SQL — on every
// row the statement would be bloat.
func TestAnOrdinaryQueryIsADebugLineWithoutSQL(t *testing.T) {
	openLoggedFixture(t, settings(10_000, true, true))
	if _, err := store.RunQuery("folder:inbox", 10, 0); err != nil {
		t.Fatal(err)
	}

	for _, l := range recorded(t) {
		if l.Level != store.LevelDebug {
			t.Errorf("an ordinary query is %q, want debug", l.Level)
		}
		if strings.Contains(l.Message+l.Context, "SELECT") {
			t.Errorf("an ordinary query carries SQL it does not need: %+v", l)
		}
	}
}

// Zero is off, not "everything is slow". Somebody who sets it to zero means
// "stop warning me about slow queries".
func TestAZeroThresholdTurnsTheWarningOff(t *testing.T) {
	openLoggedFixture(t, settings(0, true, true))
	if _, err := store.RunQuery("folder:inbox", 10, 0); err != nil {
		t.Fatal(err)
	}

	lines := recorded(t)
	if len(lines) == 0 {
		t.Fatal("a zero threshold stopped the line entirely; it only stops the warning")
	}
	for _, l := range lines {
		if l.Level == store.LevelWarn {
			t.Errorf("a zero threshold still warned: %+v", l)
		}
	}
}

// # The privacy switch
//
// A query can name a person or a subject somebody searched for, and the log
// outlives the query. Off means the words are not written at all — not
// redacted, because a redaction that leaves the shape of a query is still a
// record of what was searched for.
func TestQueryTextCanBeKeptOut(t *testing.T) {
	openLoggedFixture(t, settings(500, false, true))

	if _, err := store.RunQuery("from:solicitor@example.com", 10, 0); err != nil {
		t.Fatal(err)
	}

	lines := recorded(t)
	if len(lines) == 0 {
		t.Fatal("the whole line went; only the words should")
	}
	for _, l := range lines {
		if strings.Contains(l.Reference+l.Message+l.Context, "solicitor") {
			t.Errorf("the query text was recorded anyway: %+v", l)
		}
	}
	// The timing is why the line exists and has to survive the switch.
	if lines[0].Duration == nil {
		t.Error("turning off the words took the timing with them")
	}
}

// What makes debug usable. It is otherwise all-or-nothing: turn it up to chase
// one thing and ten thousand lines bury it.
func TestASwitchedOffCategoryRecordsNothing(t *testing.T) {
	openLoggedFixture(t, settings(500, true, false))

	if _, err := store.RunQuery("folder:inbox", 10, 0); err != nil {
		t.Fatal(err)
	}
	if n := len(recorded(t)); n != 0 {
		t.Errorf("a switched-off category recorded %d lines", n)
	}
}

// A query that did not compile is visible in the interface and gone the moment
// the box is retyped, which is exactly when somebody wants to know what it was.
func TestAFailedQueryIsRecorded(t *testing.T) {
	openLoggedFixture(t, settings(500, true, true))

	if _, err := store.RunQuery("subject:(", 10, 0); err == nil {
		t.Fatal("that query was supposed to fail")
	}

	lines := recorded(t)
	if len(lines) == 0 {
		t.Fatal("a failed query recorded nothing")
	}
	if lines[0].Level != store.LevelWarn {
		t.Errorf("a failed query is %q, want warn", lines[0].Level)
	}
}

// Settings written by an older build know nothing about a key added later, and
// one missing key must not quietly undo the others.
func TestSettingsFallBackPerField(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)

	if err := store.UpdateSetting("log.slowMs", "250"); err != nil {
		t.Fatal(err)
	}

	got := store.LoadLogSettings()
	if got.SlowMs != 250 {
		t.Errorf("slowMs = %d, want the stored 250", got.SlowMs)
	}
	if got.Retain != store.MaxLogLines {
		t.Errorf("retain = %d, want the default", got.Retain)
	}
	if !got.QueryText {
		t.Error("queryText defaulted to off")
	}
	// The noisiest by an order of magnitude, and the least often what somebody
	// turning logging up is looking for.
	if got.Categories["http"] {
		t.Error("http defaults to on")
	}
}

var _ = time.Second
