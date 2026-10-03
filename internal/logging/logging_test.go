package logging

import (
	"log"
	"log/slog"
	"testing"
	"time"

	"github.com/user/inboxql/internal/store"
)

// # What this package is for
//
// There was a table with one producer — the importer — and eighty-odd
// `log.Printf` calls going to stderr, so the UI showed one subsystem's
// failures while every sync, annotator run and model call was invisible.
//
// So the test that matters most is that an untouched `log.Println` lands in the
// table: that is the eighty-one call sites nobody is going to rewrite.
func TestTheStandardLibrarysLogLandsInTheTable(t *testing.T) {
	openLogFixture(t)

	log.Println("something the application did")
	flush(t)

	lines := read(t, store.ErrorQuery{})
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Message != "something the application did" {
		t.Errorf("message = %q", lines[0].Message)
	}
	// Info, not a guess from the text: the call sites say nothing about
	// severity and reading it out of the prose would be wrong often enough to
	// matter.
	if lines[0].Level != store.LevelInfo {
		t.Errorf("level = %q, want info", lines[0].Level)
	}
	if lines[0].Category != store.ErrorCategoryApp {
		t.Errorf("category = %q, want app", lines[0].Category)
	}
}

// Attributes this package knows about become columns, which is what makes
// `in:logs job:x` and `| count by category` possible.
func TestKnownAttributesBecomeColumns(t *testing.T) {
	openLogFixture(t)

	slog.Error("the sync failed", "category", "sync", "account", "work",
		"job", "job-7", "where", "INBOX", "ref", "uid/42")
	flush(t)

	got := read(t, store.ErrorQuery{})[0]
	for _, c := range []struct{ name, got, want string }{
		{"level", got.Level, store.LevelError},
		{"category", got.Category, "sync"},
		{"account", got.AccountID, "work"},
		{"job", got.JobID, "job-7"},
		{"context", got.Context, "INBOX"},
		{"reference", got.Reference, "uid/42"},
		{"message", got.Message, "the sync failed"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// An attribute with no column of its own is appended rather than dropped. A
// second table for them would be a join nobody wants; losing them would make
// structured logging worse than the printf it replaced.
func TestUnknownAttributesSurviveInTheMessage(t *testing.T) {
	openLogFixture(t)

	slog.Info("fetched", "messages", 42, "elapsed", "1.2s")
	flush(t)

	msg := read(t, store.ErrorQuery{})[0].Message
	for _, want := range []string{"messages=42", "elapsed=1.2s"} {
		if !contains(msg, want) {
			t.Errorf("message %q lost %q", msg, want)
		}
	}
}

// The floor is a floor: below it, nothing is stored. This is also the volume
// control, which is why it matters that it actually stops the write rather
// than filtering on read.
func TestLevelStopsLinesBeingStored(t *testing.T) {
	openLogFixture(t)

	SetLevel(store.LevelWarn)
	slog.Debug("not this")
	slog.Info("nor this")
	slog.Warn("this one")
	slog.Error("and this")
	flush(t)

	lines := read(t, store.ErrorQuery{})
	if len(lines) != 2 {
		msgs := []string{}
		for _, l := range lines {
			msgs = append(msgs, l.Message)
		}
		t.Fatalf("stored %v, want only the warning and the error", msgs)
	}
}

// Comparable, so `level>warn` is "worse than a warning" — the query somebody
// actually wants when something has gone wrong.
func TestMinLevelFiltersOnRead(t *testing.T) {
	openLogFixture(t)

	SetLevel(store.LevelDebug)
	slog.Debug("d")
	slog.Info("i")
	slog.Warn("w")
	slog.Error("e")
	flush(t)

	if n := len(read(t, store.ErrorQuery{MinLevel: store.LevelWarn})); n != 2 {
		t.Errorf("level>=warn gave %d lines, want 2", n)
	}
	if n := len(read(t, store.ErrorQuery{MinLevel: store.LevelDebug})); n != 4 {
		t.Errorf("level>=debug gave %d lines, want 4", n)
	}
}

// A line is never worth stalling a sync for. A full buffer drops rather than
// blocks — and says so, because a burst that overran the buffer would
// otherwise look like a quiet period.
func TestAFullBufferDropsRatherThanBlocks(t *testing.T) {
	// No writer: the point is what enqueue does when nothing is draining, and
	// with the real one running the buffer never fills.
	reset()
	lines = make(chan *store.LoggedError, 4)
	t.Cleanup(reset)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			enqueue(&store.LoggedError{Level: store.LevelInfo, Message: "x", CreatedAt: time.Now()})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue blocked on a full buffer; a log line stalled the caller")
	}

	if Dropped() != 16 {
		t.Errorf("dropped %d of 20 into a buffer of 4, want 16", Dropped())
	}
}

// The gap is reported rather than left silent, or a burst that overran the
// buffer reads as a quiet period — which is the opposite of what happened.
func TestDroppedLinesAreReported(t *testing.T) {
	openLogFixture(t)

	dropped.Store(3)
	slog.Info("something")
	flush(t)

	var said bool
	for _, l := range read(t, store.ErrorQuery{}) {
		if contains(l.Message, "3 lines dropped") && l.Level == store.LevelWarn {
			said = true
		}
	}
	if !said {
		t.Error("the dropped lines were not reported")
	}
}

func TestPruneKeepsTheNewest(t *testing.T) {
	openLogFixture(t)

	base := time.Now().Add(-time.Hour)
	batch := []*store.LoggedError{}
	for i := 0; i < 10; i++ {
		batch = append(batch, &store.LoggedError{
			Level: store.LevelInfo, Category: store.ErrorCategoryApp,
			Message: string(rune('a' + i)), CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	if err := store.LogLines(batch); err != nil {
		t.Fatal(err)
	}

	removed, err := store.PruneLog(3)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 7 {
		t.Errorf("removed %d, want 7", removed)
	}
	kept := read(t, store.ErrorQuery{})
	if len(kept) != 3 || kept[0].Message != "j" {
		t.Errorf("kept %d lines starting %q, want the newest three", len(kept), kept[0].Message)
	}
}

// --- fixture ---------------------------------------------------------------

// openLogFixture gives each test its own database and a fresh writer.
//
// The package's state is process-wide — one default logger, one buffer — so
// the sync.Once is reset rather than worked around. A test that silently
// shared another test's writer would pass against a broken package.
func openLogFixture(t *testing.T) {
	t.Helper()
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)

	reset()
	Start()
	SetLevel(store.LevelDebug)
	t.Cleanup(func() {
		Stop()
		reset()
	})
}

func flush(t *testing.T) {
	t.Helper()
	Stop()
	// Started again so a test may keep logging after checking.
	resetStopOnly()
}

func read(t *testing.T, q store.ErrorQuery) []*store.LoggedError {
	t.Helper()
	got, err := store.ListErrors(q)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
